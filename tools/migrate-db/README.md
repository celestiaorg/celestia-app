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
1. Create a backup of the entire `data` directory to `data_backup`
2. Create a `data_pebble` directory in `~/.celestia-app/`
3. Migrate all databases to PebbleDB format
4. Verify the integrity of migrated databases
5. Display instructions for completing the migration

### Options

- `--home <path>` - Specify custom home directory (default: `~/.celestia-app`)
- `--dry-run` - Test the migration without making changes
- `--no-backup` - Skip creating backup of data directory (not recommended)
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

The tool will ask for confirmation before proceeding. Type `y` or `yes` to continue.

### 3. Update Configuration

Edit `~/.celestia-app/config/config.toml`:

```toml
[db]
backend = "pebbledb"
```

### 4. Move Databases

```bash
cd ~/.celestia-app

# Remove old databases
rm -rf data/application.db
rm -rf data/blockstore.db
rm -rf data/state.db
rm -rf data/tx_index.db
rm -rf data/evidence.db

# Move PebbleDB files
mv data_pebble/application.db data/application.db
mv data_pebble/blockstore.db data/blockstore.db
mv data_pebble/state.db data/state.db
mv data_pebble/tx_index.db data/tx_index.db
mv data_pebble/evidence.db data/evidence.db
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

# Remove backup directory
rm -rf ~/.celestia-app/data_backup
```

## Troubleshooting

### Migration Fails Mid-Process

If migration fails partway through, simply run it again. The tool will create a fresh `data_pebble` directory.

### Node Won't Start After Migration

1. Check logs:
   ```bash
   journalctl -u celestia-appd -n 100
   ```

2. Verify config.toml has the correct backend:
   ```bash
   cat ~/.celestia-app/config/config.toml | grep backend
   ```

3. Restore from backup if needed:
   ```bash
   cd ~/.celestia-app
   rm -rf data
   mv data_backup data
   ```

   Then change config.toml back to `backend = "goleveldb"` and restart.

### Insufficient Disk Space

Ensure you have at least 2x your current data size available:

```bash
# Check data size
du -sh ~/.celestia-app/data

# Check available space
df -h ~/.celestia-app
```
