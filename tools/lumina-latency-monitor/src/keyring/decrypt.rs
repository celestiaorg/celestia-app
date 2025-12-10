//! JWE decryption for Cosmos SDK keyring files.
//!
//! Keyring files use JWE (JSON Web Encryption) with:
//! - Algorithm: PBES2-HS256+A128KW (PBKDF2 key wrapping)
//! - Encryption: A256GCM (AES-256-GCM content encryption)
//! - PBKDF2 iterations: 8192

use josekit::jwe::PBES2_HS256_A128KW;

use super::error::{KeyringError, Result};

/// Decrypt a JWE-encrypted keyring file.
///
/// The test backend uses "test" as the password.
/// The file backend requires user-provided password.
pub fn decrypt_jwe(jwe_compact: &str, password: &str) -> Result<Vec<u8>> {
    // Parse header to verify format
    let parts: Vec<&str> = jwe_compact.split('.').collect();
    if parts.len() != 5 {
        return Err(KeyringError::DecryptionFailed(
            "invalid JWE format: expected 5 parts".to_string(),
        ));
    }

    // Create PBES2 decrypter with the password
    let decrypter = PBES2_HS256_A128KW
        .decrypter_from_bytes(password.as_bytes())
        .map_err(|e| KeyringError::DecryptionFailed(e.to_string()))?;

    // Deserialize and decrypt
    let (payload, _header) = josekit::jwe::deserialize_compact(jwe_compact, &decrypter)
        .map_err(|e| KeyringError::DecryptionFailed(e.to_string()))?;

    Ok(payload)
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
