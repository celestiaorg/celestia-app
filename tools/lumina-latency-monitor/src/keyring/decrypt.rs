//! JWE decryption for Cosmos SDK keyring files.
//!
//! Keyring files use JWE (JSON Web Encryption) with:
//! - Algorithm: PBES2-HS256+A128KW (PBKDF2 key wrapping)
//! - Encryption: A256GCM (AES-256-GCM content encryption)
//!
//! This implementation uses pure-Rust crates (no OpenSSL).

use aes_gcm::aead::generic_array::typenum::Unsigned;
use aes_gcm::{
    aead::{Aead, AeadCore, KeyInit},
    Aes256Gcm, Nonce,
};
use aes_kw::KekAes128;
use base64::{engine::general_purpose::URL_SAFE_NO_PAD, Engine};
use hmac::Hmac;
use pbkdf2::pbkdf2;
use sha2::Sha256;

use super::error::{KeyringError, Result};

/// JWE header for PBES2-HS256+A128KW
#[derive(serde::Deserialize)]
struct JweHeader {
    alg: String,
    enc: String,
    p2c: u32,    // PBKDF2 iteration count
    p2s: String, // PBKDF2 salt (base64url)
}

/// Decrypt a JWE-encrypted keyring file.
///
/// The test backend uses "test" as the password.
/// The file backend requires user-provided password.
pub fn decrypt_jwe(jwe_compact: &str, password: &str) -> Result<Vec<u8>> {
    // Parse JWE compact serialization: header.encrypted_key.iv.ciphertext.tag
    let parts: Vec<&str> = jwe_compact.split('.').collect();
    if parts.len() != 5 {
        return Err(KeyringError::DecryptionFailed(
            "invalid JWE format: expected 5 parts".to_string(),
        ));
    }

    let header_b64 = parts[0];
    let encrypted_key_b64 = parts[1];
    let iv_b64 = parts[2];
    let ciphertext_b64 = parts[3];
    let tag_b64 = parts[4];

    // Decode header
    let header_json = URL_SAFE_NO_PAD
        .decode(header_b64)
        .map_err(|e| KeyringError::DecryptionFailed(format!("header decode: {}", e)))?;
    let header: JweHeader = serde_json::from_slice(&header_json)
        .map_err(|e| KeyringError::DecryptionFailed(format!("header parse: {}", e)))?;

    // Verify algorithm
    if header.alg != "PBES2-HS256+A128KW" {
        return Err(KeyringError::DecryptionFailed(format!(
            "unsupported algorithm: {}",
            header.alg
        )));
    }
    if header.enc != "A256GCM" {
        return Err(KeyringError::DecryptionFailed(format!(
            "unsupported encryption: {}",
            header.enc
        )));
    }

    // Decode other parts
    let encrypted_key = URL_SAFE_NO_PAD
        .decode(encrypted_key_b64)
        .map_err(|e| KeyringError::DecryptionFailed(format!("encrypted_key decode: {}", e)))?;
    let iv = URL_SAFE_NO_PAD
        .decode(iv_b64)
        .map_err(|e| KeyringError::DecryptionFailed(format!("iv decode: {}", e)))?;
    let ciphertext = URL_SAFE_NO_PAD
        .decode(ciphertext_b64)
        .map_err(|e| KeyringError::DecryptionFailed(format!("ciphertext decode: {}", e)))?;
    let tag = URL_SAFE_NO_PAD
        .decode(tag_b64)
        .map_err(|e| KeyringError::DecryptionFailed(format!("tag decode: {}", e)))?;

    // Decode PBKDF2 salt from header
    let p2s = URL_SAFE_NO_PAD
        .decode(&header.p2s)
        .map_err(|e| KeyringError::DecryptionFailed(format!("p2s decode: {}", e)))?;

    // Derive KEK using PBKDF2
    // For PBES2-HS256+A128KW, the salt is: algorithm || 0x00 || p2s
    let alg_id = b"PBES2-HS256+A128KW";
    let mut salt = Vec::with_capacity(alg_id.len() + 1 + p2s.len());
    salt.extend_from_slice(alg_id);
    salt.push(0x00);
    salt.extend_from_slice(&p2s);

    // Derive 16-byte key for A128KW
    let mut kek = [0u8; 16];
    pbkdf2::<Hmac<Sha256>>(password.as_bytes(), &salt, header.p2c, &mut kek)
        .map_err(|e| KeyringError::DecryptionFailed(format!("pbkdf2: {}", e)))?;

    // Unwrap the CEK using AES-KW
    let kek = KekAes128::from(kek);
    let mut cek = [0u8; 32]; // A256GCM needs 32-byte key
    kek.unwrap(&encrypted_key, &mut cek)
        .map_err(|e| KeyringError::DecryptionFailed(format!("key unwrap: {:?}", e)))?;

    // Decrypt content using AES-256-GCM
    let cipher = Aes256Gcm::new_from_slice(&cek)
        .map_err(|e| KeyringError::DecryptionFailed(format!("cipher init: {}", e)))?;

    // GCM nonce is the IV
    let expected_nonce_len = <Aes256Gcm as AeadCore>::NonceSize::USIZE;
    if iv.len() != expected_nonce_len {
        return Err(KeyringError::DecryptionFailed(format!(
            "invalid iv length: expected {}, got {}",
            expected_nonce_len,
            iv.len()
        )));
    }
    let nonce: Nonce<<Aes256Gcm as AeadCore>::NonceSize> = iv.iter().copied().collect();

    // Combine ciphertext and tag for decryption
    let mut ciphertext_with_tag = ciphertext;
    ciphertext_with_tag.extend_from_slice(&tag);

    // AAD is the protected header (base64url encoded)
    let plaintext = cipher
        .decrypt(
            &nonce,
            aes_gcm::aead::Payload {
                msg: &ciphertext_with_tag,
                aad: header_b64.as_bytes(),
            },
        )
        .map_err(|e| KeyringError::DecryptionFailed(format!("decrypt: {}", e)))?;

    Ok(plaintext)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_decrypt_invalid_format() {
        let result = decrypt_jwe("not.enough.parts", "test");
        assert!(result.is_err());
    }
}
