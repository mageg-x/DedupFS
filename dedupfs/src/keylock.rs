use std::sync::Mutex;
use std::collections::hash_map::DefaultHasher;
use std::hash::{Hash, Hasher};
use lazy_static::lazy_static; // 你已引入

const SHARD_COUNT: usize = 1024;

lazy_static! {
    static ref KEY_LOCKS: [Mutex<()>; SHARD_COUNT] = {
        std::array::from_fn(|_| Mutex::new(()))
    };
}

#[inline]
fn shard_index(key: &str) -> usize {
    let mut hasher = DefaultHasher::new();
    key.hash(&mut hasher);
    (hasher.finish() as usize) % SHARD_COUNT
}

pub fn lock_key(key: &str) -> std::sync::MutexGuard<'static, ()> {
    KEY_LOCKS[shard_index(key)].lock().unwrap()
}

pub fn try_lock_key(key: &str) -> Option<std::sync::MutexGuard<'static, ()>> {
    KEY_LOCKS[shard_index(key)].try_lock().ok()
}