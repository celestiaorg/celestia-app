# migrate-db

A tool to migrate Celestia App databases from LevelDB to PebbleDB.

## Overview

This tool migrates all celestia-app databases from LevelDB format to PebbleDB format. PebbleDB offers better performance and scalability compared to LevelDB.

## Installation

```bash
cd tools/migrate-db
go build -o migrate-db
```

## Usage

### Basic Migration

```bash
./migrate-db
```

This will:
1. Create a `data_pebble` directory in `~/.celestia-app/`
2. Migrate all databases to PebbleDB format
3. Display instructions for completing the migration

### Options

- `--home <path>` - Specify custom home directory (default: `~/.celestia-app`)
- `--dry-run` - Test the migration without making changes
- `--backup=false` - Skip creating backups (not recommended)
- `--cleanup` - Remove old LevelDB files after migration (use with caution)

### Examples

**Dry-run (test without changes):**
```bash
./migrate-db --dry-run
```

**Custom home directory:**
```bash
./migrate-db --home /custom/path/.celestia-app
```

**Migration without backup (not recommended):**
```bash
./migrate-db --backup=false
```

## Migrated Databases

The tool migrates the following databases:

- `application.db` - Application state (usually the largest)
- `blockstore.db` - Block storage
- `state.db` - Consensus state
- `tx_index.db` - Transaction index
- `evidence.db` - Evidence storage

Files NOT migrated (will remain unchanged):
- `cs.wal` - Consensus write-ahead log (recreated automatically)
- `priv_validator_state.json` - Validator state file (JSON)
- `snapshots/` - State sync snapshots directory
- `traces/` - Trace files directory

## Complete Migration Process

### 1. Stop Your Node

```bash
sudo systemctl stop celestia-appd
```

### 2. Run Migration

```bash
cd /path/to/celestia-app/tools/migrate-db
go build
./migrate-db
```

### 3. Update Configuration

Edit `~/.celestia-app/config/config.toml`:

```toml
[db]
backend = "pebbledb"
```

### 4. Move Databases

```bash
cd ~/.celestia-app

# Backup originals (if not already done)
mv data/application.db data/application.db.backup
mv data/blockstore.db data/blockstore.db.backup
mv data/state.db data/state.db.backup
mv data/tx_index.db data/tx_index.db.backup
mv data/evidence.db data/evidence.db.backup

# Move PebbleDB files
mv data_pebble/*.db data/
```

### 5. Start Your Node

```bash
sudo systemctl start celestia-appd
```

### 6. Verify

```bash
# Check status
celestia-appd status

# Monitor logs
journalctl -u celestia-appd -f
```

### 7. Cleanup (After Verification)

After confirming everything works for a few days:

```bash
# Remove migration directory
rm -rf ~/.celestia-app/data_pebble

# Remove backups
rm -rf ~/.celestia-app/data/*.backup
rm -rf ~/.celestia-app/data/*.leveldb.backup
```

## Troubleshooting

### Migration Fails Mid-Process

If migration fails partway through, simply run it again. The tool will create a fresh `data_pebble` directory.

### Node Won't Start After Migration

1. Check logs:
   ```bash
   journalctl -u celestia-appd -n 100
   ```

2. Verify config.toml has correct backend:
   ```bash
   cat ~/.celestia-app/config/config.toml | grep backend
   ```

3. Restore from backup if needed:
   ```bash
   cd ~/.celestia-app/data
   rm -rf *.db
   mv application.db.backup application.db
   mv blockstore.db.backup blockstore.db
   mv state.db.backup state.db
   mv tx_index.db.backup tx_index.db
   mv evidence.db.backup evidence.db
   ```

### Insufficient Disk Space

Ensure you have at least 2x your current data size available:

```bash
# Check data size
du -sh ~/.celestia-app/data

# Check available space
df -h ~/.celestia-app
```

## Performance Notes

Migration time depends on database size:

- Small node (< 100GB): 30-60 minutes
- Medium node (100-500GB): 1-3 hours
- Large node (> 500GB): 3-6+ hours

Progress is displayed every 10,000 keys migrated.

## Safety Features

- ✅ Automatic backups before migration
- ✅ Data integrity verification after migration
- ✅ Dry-run mode for testing
- ✅ No modification of original data
- ✅ Rollback capability via backups

## Support

For issues or questions, please open an issue at:
https://github.com/celestiaorg/celestia-app/issues
