//! Cosmos SDK keyring support for reading keys from ~/.celestia-app/keyring-test/
//!
//! This module provides read-only access to Cosmos SDK keyrings, matching the
//! behavior of the Go `keyring` package used by celestia-app tools.

mod decrypt;
mod error;
mod proto;

pub use error::{KeyringError, Result};

use std::fs;
use std::path::{Path, PathBuf};

use base64::prelude::*;
use bech32::{Bech32, Hrp};
use prost::Message;
use ripemd::Ripemd160;
use serde::Deserialize;
use sha2::{Digest, Sha256};

use decrypt::decrypt_jwe;
use proto::{Record, Secp256k1PrivKey, Secp256k1PubKey};

/// Password used by the Cosmos SDK test backend
const TEST_BACKEND_PASSWORD: &str = "test";

/// Keyring backend type
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Backend {
    /// Test backend - uses hardcoded password "test"
    Test,
}

/// Key record from keyring (public info only)
#[derive(Debug, Clone)]
pub struct KeyRecord {
    pub name: String,
    pub address: String,
    pub public_key: Vec<u8>,
}

/// Local key with private key material
#[derive(Debug, Clone)]
pub struct LocalKey {
    pub record: KeyRecord,
    pub private_key: Vec<u8>,
}

impl LocalKey {
    /// Returns private key as hex string (for celestia-grpc compatibility)
    pub fn private_key_hex(&self) -> String {
        hex::encode(&self.private_key)
    }
}

/// JSON wrapper used by 99designs/keyring library
#[derive(Deserialize)]
struct KeyringItem {
    #[serde(rename = "Key")]
    key: String,
    #[serde(rename = "Data")]
    data: String,
}

/// File-based keyring reader
pub struct FileKeyring {
    dir: PathBuf,
    backend: Backend,
    password: String,
}

impl FileKeyring {
    /// Open a keyring at the specified directory
    pub fn open(base_dir: impl AsRef<Path>, backend: Backend) -> Result<Self> {
        let base_dir = expand_tilde(base_dir.as_ref())?;
        let subdir = match backend {
            Backend::Test => "keyring-test",
        };
        let dir = base_dir.join(subdir);

        if !dir.exists() {
            return Err(KeyringError::DirectoryNotFound(dir));
        }

        let password = match backend {
            Backend::Test => TEST_BACKEND_PASSWORD.to_string(),
        };

        Ok(Self {
            dir,
            backend,
            password,
        })
    }

    /// Open keyring at default location (~/.celestia-app)
    pub fn open_default(backend: Backend) -> Result<Self> {
        Self::open("~/.celestia-app", backend)
    }

    /// Returns the backend type
    pub fn backend(&self) -> Backend {
        self.backend
    }

    /// List all key names in the keyring
    pub fn list(&self) -> Result<Vec<String>> {
        let mut names = Vec::new();
        for entry in fs::read_dir(&self.dir)? {
            let path = entry?.path();
            if path.extension().is_some_and(|e| e == "info") {
                if let Some(name) = path.file_stem() {
                    names.push(name.to_string_lossy().to_string());
                }
            }
        }
        names.sort();
        Ok(names)
    }

    /// Get the first key name (default behavior like Go tool)
    pub fn first_key(&self) -> Result<String> {
        self.list()?
            .into_iter()
            .next()
            .ok_or(KeyringError::NoKeysFound)
    }

    /// Get key record by name (public info only)
    pub fn key(&self, name: &str) -> Result<KeyRecord> {
        let local = self.local_key(name)?;
        Ok(local.record)
    }

    /// Get full local key with private key material
    pub fn local_key(&self, name: &str) -> Result<LocalKey> {
        let info_path = self.dir.join(format!("{}.info", name));
        let jwe = fs::read_to_string(&info_path)
            .map_err(|_| KeyringError::KeyNotFound(name.to_string()))?;

        // Decrypt JWE to get JSON wrapper
        let decrypted = decrypt_jwe(&jwe, &self.password)?;

        // Parse JSON wrapper from 99designs/keyring
        let item: KeyringItem = serde_json::from_slice(&decrypted)
            .map_err(|e| KeyringError::ProtobufError(format!("invalid JSON wrapper: {}", e)))?;

        // Base64 decode the Data field to get protobuf bytes
        let proto_bytes = BASE64_STANDARD
            .decode(&item.data)
            .map_err(|e| KeyringError::ProtobufError(format!("invalid base64: {}", e)))?;

        // Parse protobuf Record
        let record = Record::decode(proto_bytes.as_slice())
            .map_err(|e| KeyringError::ProtobufError(e.to_string()))?;

        // Extract public key
        let pub_key_any = record.pub_key.ok_or(KeyringError::MissingPublicKey)?;
        let pub_key = Secp256k1PubKey::decode(pub_key_any.value.as_slice())
            .map_err(|e| KeyringError::ProtobufError(e.to_string()))?;

        // Extract private key from Local variant
        let local_info = record
            .local
            .ok_or_else(|| KeyringError::NotLocalKey(name.to_string()))?;
        let priv_key_any = local_info.priv_key.ok_or(KeyringError::MissingPrivateKey)?;

        // Verify it's a secp256k1 key
        if !priv_key_any.type_url.contains("secp256k1") {
            return Err(KeyringError::UnsupportedKeyType(priv_key_any.type_url));
        }

        let priv_key = Secp256k1PrivKey::decode(priv_key_any.value.as_slice())
            .map_err(|e| KeyringError::ProtobufError(e.to_string()))?;

        // Derive bech32 address from public key
        let address = derive_address(&pub_key.key, "celestia")?;

        Ok(LocalKey {
            record: KeyRecord {
                name: record.name,
                address,
                public_key: pub_key.key,
            },
            private_key: priv_key.key,
        })
    }
}

/// Expand ~ to home directory
fn expand_tilde(path: &Path) -> Result<PathBuf> {
    let path_str = path.to_string_lossy();
    if let Some(rest) = path_str.strip_prefix("~/") {
        let home = dirs::home_dir().ok_or(KeyringError::HomeDirNotFound)?;
        Ok(home.join(rest))
    } else if path_str == "~" {
        dirs::home_dir().ok_or(KeyringError::HomeDirNotFound)
    } else {
        Ok(path.to_path_buf())
    }
}

/// Derive bech32 address from compressed secp256k1 public key
/// Uses: RIPEMD160(SHA256(pubkey))
fn derive_address(public_key: &[u8], prefix: &str) -> Result<String> {
    // SHA256 hash
    let sha_hash = Sha256::digest(public_key);

    // RIPEMD160 hash
    let ripe_hash = Ripemd160::digest(sha_hash);

    // Bech32 encode
    let hrp = Hrp::parse(prefix).map_err(|e| KeyringError::AddressError(e.to_string()))?;
    let address = bech32::encode::<Bech32>(hrp, &ripe_hash)
        .map_err(|e| KeyringError::AddressError(e.to_string()))?;

    Ok(address)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_expand_tilde() {
        let home = dirs::home_dir().unwrap();
        assert_eq!(expand_tilde(Path::new("~/foo")).unwrap(), home.join("foo"));
        assert_eq!(
            expand_tilde(Path::new("/absolute/path")).unwrap(),
            PathBuf::from("/absolute/path")
        );
    }
}
