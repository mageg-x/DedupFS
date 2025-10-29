package utils

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	mrand "math/rand"
	"path"
	"runtime"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
	"golang.org/x/crypto/pbkdf2"
)

// DumpCaller 打印多层调用栈
func DumpCaller(depth int) {
	fmt.Printf("[CALLER STACK]\n")
	for i := 1; i <= depth; i++ {
		if pc, file, line, ok := runtime.Caller(i); ok {
			_, filename := path.Split(file)
			fn := runtime.FuncForPC(pc)
			fmt.Printf("  [%d] %s:%d %s\n", i, filename, line, fn.Name())
		}
	}
}

func RetryCall(maxRetries int, fn func() error) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		if i < maxRetries-1 {
			base := 500 * time.Millisecond
			delay := base * time.Duration(1<<uint(i)) // 500ms, 1s, 2s, 4s...
			sleep := time.Duration(mrand.Int63n(int64(delay)))
			time.Sleep(sleep)
		}
	}
	return fmt.Errorf("retry failed after %d attempts: %w", maxRetries, lastErr)
}

// WithLock 是一个辅助函数，用于在特定代码块内自动锁定和解锁
func WithLock(mu *sync.Mutex, fn func() error) error {
	mu.Lock()
	defer mu.Unlock()
	return fn()
}

func WithTryLock(mu *sync.Mutex, fn func() error) error {
	if mu.TryLock() {
		defer mu.Unlock()
		return fn()
	} else {
		return errors.New("try lock fail")
	}
}

func WrapFunction(fn func() error) error {
	return fn()
}

func GenKey(password string, keyLen int) []byte {
	// 实际应用中，salt 应随机生成并随密文一起存储
	salt := []byte("liusiming@rao") // 至少 8 字节

	// 迭代次数（建议 100,000 以上）
	iterations := 100

	key := pbkdf2.Key([]byte(password), salt, iterations, keyLen, sha256.New)
	return key
}

// Compress 压缩函数 - 使用Zstd
func Compress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}

	var compressed bytes.Buffer
	encoder, err := zstd.NewWriter(&compressed, zstd.WithEncoderLevel(zstd.SpeedDefault)) // 直接传 buffer
	if err != nil {
		return nil, err
	}

	// 写入数据
	_, err = encoder.Write(data)
	if err != nil {
		encoder.Close() // 尽量关闭
		return nil, err
	}

	//关键：必须 Close 才会 flush 剩余数据
	err = encoder.Close()
	if err != nil {
		return nil, err
	}

	// 此时 compressed 里才有完整数据
	return compressed.Bytes(), nil
}

// Decompress 解压缩函数 - 使用Zstd
func Decompress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return data, nil
	}

	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	defer decoder.Close()

	var decompressed bytes.Buffer
	decoder.Reset(bytes.NewReader(data))
	_, err = io.Copy(&decompressed, decoder)
	if err != nil {
		return nil, err
	}

	return decompressed.Bytes(), nil
}

// Encrypt 加密函数 - 使用AES-GCM
func Encrypt(data []byte, key string) ([]byte, error) {
	keyBytes := GenKey(key, 16)

	// 创建 AES cipher
	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, err
	}

	// 创建 GCM 模式
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// 生成随机 nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	// 加密
	ciphertext := gcm.Seal(nonce, nonce, data, nil)

	return ciphertext, nil
}

// Decrypt  解密函数
func Decrypt(data []byte, key string) ([]byte, error) {
	keyBytes := GenKey(key, 16)

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, encryptedData := data[:nonceSize], data[nonceSize:]

	// 解密（自动验证认证标签）
	plaintext, err := gcm.Open(nil, nonce, encryptedData, nil)
	if err != nil {
		return nil, errors.New("decryption failed: invalid key or corrupted data")
	}

	return plaintext, nil
}
