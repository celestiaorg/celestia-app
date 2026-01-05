//! Protobuf message definitions for Cosmos SDK keyring records.
//!
//! These match the cosmos-sdk proto definitions:
//! - cosmos/crypto/keyring/v1/record.proto
//! - cosmos/crypto/secp256k1/keys.proto

use prost::Message;

/// google.protobuf.Any - wrapper for typed protobuf messages
#[derive(Clone, PartialEq, Message)]
pub struct Any {
    #[prost(string, tag = "1")]
    pub type_url: String,
    #[prost(bytes = "vec", tag = "2")]
    pub value: Vec<u8>,
}

/// cosmos.crypto.keyring.v1.Record - main key record
#[derive(Clone, PartialEq, Message)]
pub struct Record {
    #[prost(string, tag = "1")]
    pub name: String,
    #[prost(message, optional, tag = "2")]
    pub pub_key: Option<Any>,
    #[prost(message, optional, tag = "3")]
    pub local: Option<LocalInfo>,
    // tag 4: Ledger (not implemented)
    // tag 5: Multi (not implemented)
    // tag 6: Offline (not implemented)
}

/// cosmos.crypto.keyring.v1.Record.Local - local key storage
#[derive(Clone, PartialEq, Message)]
pub struct LocalInfo {
    #[prost(message, optional, tag = "1")]
    pub priv_key: Option<Any>,
}

/// cosmos.crypto.secp256k1.PubKey
#[derive(Clone, PartialEq, Message)]
pub struct Secp256k1PubKey {
    #[prost(bytes = "vec", tag = "1")]
    pub key: Vec<u8>,
}

/// cosmos.crypto.secp256k1.PrivKey
#[derive(Clone, PartialEq, Message)]
pub struct Secp256k1PrivKey {
    #[prost(bytes = "vec", tag = "1")]
    pub key: Vec<u8>,
}

/// Type URL for secp256k1 public key
pub const SECP256K1_PUBKEY_TYPE_URL: &str = "/cosmos.crypto.secp256k1.PubKey";

/// Type URL for secp256k1 private key
pub const SECP256K1_PRIVKEY_TYPE_URL: &str = "/cosmos.crypto.secp256k1.PrivKey";
