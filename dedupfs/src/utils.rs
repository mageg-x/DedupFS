use aes_gcm::{
    aead::{Aead, KeyInit, OsRng},
    Aes128Gcm, Nonce
};
use hmac::Hmac;
use sha2::Sha256;
use pbkdf2::pbkdf2;
use zstd::stream::{copy_encode, copy_decode};
use std::io::Cursor;
use rand::RngCore;
use anyhow::Result;

pub fn gen_key(password: &str, key_len: usize) -> Result<Vec<u8>> {
    // 实际应用中，salt 应随机生成并随密文一起存储
    let salt = b"liusiming@rao"; // 至少 8 字节

    // 迭代次数（建议 100,000 以上）
    let iterations = 100;

    let mut key = vec![0u8; key_len];
    pbkdf2::<Hmac<Sha256>>(
        password.as_bytes(),
        salt,
        iterations,
        &mut key
    );
    
    Ok(key)
}

/// 返回: 包含nonce和加密数据的组合字节数组
pub fn encrypt_data(data: &[u8], key: &[u8]) -> Result<Vec<u8>> {
    // 确保密钥长度为16字节（AES-128）
    if key.len() != 16 {
        return Err(crate::errors::new(crate::errors::InvalidKeyLength { key_len: key.len().to_string() }));
    }
    
    // 创建AES-128-GCM加密器
    let cipher = Aes128Gcm::new_from_slice(key)?;
    
    // 生成随机nonce (96位)
    let mut nonce_bytes = [0u8; 12];
    rand::rng().fill_bytes(&mut nonce_bytes);
    let nonce = Nonce::from_slice(&nonce_bytes);
    
    // 执行加密 - 新版本中encrypt返回Result
    let encrypted_data = cipher.encrypt(nonce, data)
        .map_err(|e| crate::errors::new(crate::errors::EncryptionFailed { reason: e.to_string() }))?;
    
    // 将nonce和加密数据组合存储
    let mut combined_data = Vec::with_capacity(nonce_bytes.len() + encrypted_data.len());
    combined_data.extend_from_slice(&nonce_bytes);
    combined_data.extend_from_slice(&encrypted_data);
    
    Ok(combined_data)
}

/// 返回: 解密后的数据
pub fn decrypt_data(data: &[u8], key: &[u8]) -> Result<Vec<u8>> {
    // 确保密钥长度为16字节（AES-128）
    if key.len() != 16 {
        return Err(crate::errors::new(crate::errors::InvalidKeyLength { key_len: key.len().to_string() }));
    }
    // 提取nonce（前12字节）
    if data.len() < 12 {
        return Err(crate::errors::new(crate::errors::EncryptedDataTooShort { length: data.len().to_string() }));
    }
    
    let nonce_bytes = &data[..12];
    let encrypted_data = &data[12..];
    
    // 创建AES-128-GCM解密器
    let cipher = Aes128Gcm::new_from_slice(key)?;
    let nonce = Nonce::from_slice(nonce_bytes);
    
    // 执行解密 - 新版本中decrypt返回Result
    let decrypted_data = cipher.decrypt(nonce, encrypted_data)
        .map_err(|e| crate::errors::new(crate::errors::DecryptionFailed { reason: e.to_string() }))?;
    
    Ok(decrypted_data)
}

/// 返回: 压缩后的数据（仅当压缩有效果时）
pub fn compress_data(data: &[u8], level: i32) -> Result<Option<Vec<u8>>> {
    let mut compressed_data = Vec::new();
    let input = Cursor::new(data);
    
    // 使用zstd进行压缩
    copy_encode(input, &mut compressed_data, level)
        .map_err(|e| crate::errors::new(crate::errors::SerializationError { error: e.to_string() }))?;

    // 只有在压缩有效果时才返回压缩后的数据
    if compressed_data.len() < data.len() {
        Ok(Some(compressed_data))
    } else {
        Ok(None)
    }
}

/// 返回: 解压缩后的数据
pub fn decompress_data(data: &[u8]) -> Result<Vec<u8>> {
    let input = Cursor::new(data);
    let mut decompressed_data = Vec::new();
    
    // 使用zstd进行解压缩
    copy_decode(input, &mut decompressed_data)
        .map_err(|e| crate::errors::new(crate::errors::DeserializationError { error: e.to_string() }))?;

    Ok(decompressed_data)
}