use std::path::PathBuf;
use thiserror::Error;

#[derive(Error, Debug)]
pub enum KeyringError {
    #[error("keyring directory not found: {0}")]
    DirectoryNotFound(PathBuf),

    #[error("key not found: {0}")]
    KeyNotFound(String),

    #[error("address not found: {0}")]
    AddressNotFound(String),

    #[error("no keys in keyring")]
    NoKeysFound,

    #[error("key '{0}' is not a local key (ledger/multi/offline)")]
    NotLocalKey(String),

    #[error("private key missing in record")]
    MissingPrivateKey,

    #[error("public key missing in record")]
    MissingPublicKey,

    #[error("unsupported key type: {0}")]
    UnsupportedKeyType(String),

    #[error("decryption failed: {0}")]
    DecryptionFailed(String),

    #[error("invalid protobuf: {0}")]
    ProtobufError(String),

    #[error("address derivation failed: {0}")]
    AddressError(String),

    #[error("home directory not found")]
    HomeDirNotFound,

    #[error("IO error: {0}")]
    IoError(#[from] std::io::Error),
}

pub type Result<T> = std::result::Result<T, KeyringError>;
