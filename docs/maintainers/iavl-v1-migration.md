# IAVL v1 Migration Guide for Consensus Nodes

## Background

Starting with celestia-app v4, the state machine can use IAVL v1.x instead of v0.x. IAVL v1 introduces data locality optimizations that provide performance improvements over the previous version.

The entire database needs to be migrated to the v1 key layout. There are two migration approaches:

1. **Lazy Migration** (default): Automatic migration during normal operation
2. **State Sync Migration** (recommended): Full migration via state sync for optimal performance

## Prerequisites

Before starting the migration process:

1. **Backup critical data**: Always backup validator keys and state before any migration
2. **Plan downtime**: Especially for state sync migration, plan for node downtime

## Migration Options

### Option 1: Lazy Migration (Default)

When you upgrade to celestia-app v4, IAVL v1 migration happens automatically and lazily as your node operates. 

**Pros:**
- Node continues operating normally during migration

**Cons:**
- May cause increased I/O load during the migration period

**When to use:**
- You want a simple upgrade process

### Option 2: State Sync Migration (Recommended)

Perform a fresh state sync with celestia-app v4 to ensure all state data uses the new IAVL v1 key layout from the start.

**Pros:**
- Immediate access to full IAVL v1 performance benefits

**Cons:**
- Requires more manual intervention

**When to use:**
- You want maximum performance immediately

## Step-by-Step Instructions

### Lazy Migration Process

1. **Backup your validator state** (important):
   ```bash
   # Backup priv_validator_state.json
   cp ~/.celestia-app/data/priv_validator_state.json ~/validator_state_backup.json
   
   # Backup validator keys
   cp ~/.celestia-app/config/priv_validator_key.json ~/priv_validator_key_backup.json
   cp ~/.celestia-app/config/node_key.json ~/node_key_backup.json
   ```

2. **Stop your current node**:
   ```bash
   # Stop the celestia-app service
   sudo systemctl stop celestia-appd
   ```

3. **Upgrade to celestia-app v4**:
   ```bash
   # If installed from source (with multiplexer support)
   git checkout v4.x.x  # Replace with actual v4 release tag
   make install
   
   # For standalone binary without multiplexer (if already on v4 network)
   # make install-standalone
   
   # If using prebuilt binaries, download from releases page
   # wget https://github.com/celestiaorg/celestia-app/releases/download/v4.x.x/celestia-app_Linux_x86_64.tar.gz
   ```

   > **Note**: celestia-app v4 includes a multiplexer by default that supports syncing from genesis using embedded v3 binaries. The multiplexer will automatically switch to v4 state machine when the network upgrades. If your network has already upgraded to v4, you can use the standalone binary.

4. **Start your node with v4**:
   ```bash
   # Start the service
   sudo systemctl start celestia-appd
   
   # Monitor logs for migration progress
   journalctl -u celestia-appd -f
   ```

5. **Monitor logs**: Watch logs for completion of lazy migration.

### State Sync Migration Process

Perform a fresh state sync with celestia-app v4 to ensure all state data uses the new IAVL v1 key layout from the start. Since operators already know how to perform state sync, simply backup your validator keys and state, then perform state sync with celestia-app v4.

