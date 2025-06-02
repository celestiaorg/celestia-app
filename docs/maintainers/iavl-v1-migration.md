# IAVL v1 Migration Guide for Consensus Nodes

This guide helps consensus node operators migrate from IAVL v0 to IAVL v1 to achieve optimal performance benefits.

## Background

Starting with celestia-app v4, the state machine uses IAVL v1.x instead of v0.x. IAVL v1 introduces data locality optimizations that provide roughly **10x performance improvement** over the previous version.

### Key Benefits of IAVL v1

- **Significantly faster state access**: ~10x performance improvement
- **Better data locality**: More efficient disk I/O patterns
- **Reduced state access bottlenecks**: Prevents state access from limiting throughput

### Migration Considerations

The entire database needs to be migrated to the v1 key layout. There are two migration approaches:

1. **Lazy Migration** (default): Automatic migration during normal operation
2. **State Sync Migration** (recommended): Full migration via state sync for optimal performance

## Prerequisites

Before starting the migration process:

1. **Verify you're ready for v4**: Ensure you understand the v4 upgrade requirements from the [release notes](../release-notes/release-notes.md)
2. **Backup critical data**: Always backup validator keys and state before any migration
3. **Plan downtime**: Especially for state sync migration, plan for node downtime
4. **Monitor resources**: Ensure sufficient disk space and I/O capacity for migration
5. **Test environment**: If possible, test the migration process on a non-production node first

## Migration Options

### Option 1: Lazy Migration (Default)

When you upgrade to celestia-app v4, IAVL v1 migration happens automatically and lazily as your node operates. 

**Pros:**
- Requires no additional action from operators
- Node continues operating normally during migration

**Cons:**
- May cause increased I/O load during the migration period
- State access might become a performance bottleneck initially
- Not all data is immediately optimized for v1 layout

**When to use:**
- You want a simple upgrade process
- Your node can tolerate temporary performance degradation
- You're not concerned about maximizing throughput immediately

### Option 2: State Sync Migration (Recommended)

Perform a fresh state sync with celestia-app v4 to ensure all state data uses the new IAVL v1 key layout from the start.

**Pros:**
- Immediate access to full IAVL v1 performance benefits
- All state data is optimized for the new layout
- No performance degradation period

**Cons:**
- Requires more manual intervention
- Longer initial setup time
- Requires backing up and restoring validator keys

**When to use:**
- You want maximum performance immediately
- Your node requires optimal throughput
- You can afford the setup time for state sync

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
   # If installed from source
   git checkout v4.x.x  # Replace with actual v4 release tag
   make install
   
   # If using prebuilt binaries, download from releases page
   # wget https://github.com/celestiaorg/celestia-app/releases/download/v4.x.x/celestia-app_Linux_x86_64.tar.gz
   ```

4. **Start your node with v4**:
   ```bash
   # Start the service
   sudo systemctl start celestia-appd
   
   # Monitor logs for migration progress
   journalctl -u celestia-appd -f
   ```

5. **Monitor performance**: Watch for completion of lazy migration and performance improvements over time.

### State Sync Migration Process

1. **Prepare for state sync**:
   ```bash
   # Backup validator state and keys (CRITICAL)
   cp ~/.celestia-app/data/priv_validator_state.json ~/validator_state_backup.json
   cp ~/.celestia-app/config/priv_validator_key.json ~/priv_validator_key_backup.json
   cp ~/.celestia-app/config/node_key.json ~/node_key_backup.json
   
   # Backup config files
   cp ~/.celestia-app/config/config.toml ~/config_backup.toml
   cp ~/.celestia-app/config/app.toml ~/app_backup.toml
   ```

2. **Stop the current node**:
   ```bash
   sudo systemctl stop celestia-appd
   ```

3. **Reset node data** (keeping configs and keys):
   ```bash
   # Remove only the data directory
   rm -rf ~/.celestia-app/data
   ```

4. **Configure state sync** in `~/.celestia-app/config/config.toml`:
   ```toml
   [statesync]
   enable = true
   
   # Use trusted RPC endpoints for state sync
   # For mainnet:
   rpc_servers = "https://rpc-1.celestia.org:443,https://rpc-2.celestia.org:443"
   
   # For testnet, use appropriate testnet RPC endpoints
   # rpc_servers = "https://rpc-mocha.pops.one:443,https://rpc.celestia-mocha.com:443"
   
   # Get trust height and hash from a recent block
   # You can query: curl -s https://rpc-1.celestia.org/block | jq -r '.result.block.header.height + "," + .result.block_id.hash'
   trust_height = 0  # Replace with actual height
   trust_hash = ""   # Replace with actual hash
   
   # Optional: Configure snapshot settings
   trust_period = "112h"  # 2/3 of unbonding period
   ```

5. **Start celestia-app v4**:
   ```bash
   # Start the service
   sudo systemctl start celestia-appd
   
   # Monitor state sync progress
   journalctl -u celestia-appd -f
   ```

6. **Verify state sync completion**:
   ```bash
   # Check if the node is catching up
   celestia-appd status | jq .SyncInfo.catching_up
   
   # Should return false when sync is complete
   ```

## Performance Monitoring

### Before Migration
Monitor these metrics to establish baseline performance:

```bash
# Check block processing time
celestia-appd status | jq .SyncInfo

# Monitor system resources
htop
iostat -x 1
```

### After Migration
Compare performance improvements:

```bash
# Verify IAVL version in use
celestia-appd version --long | grep -i iavl

# Monitor block processing improvements
celestia-appd status | jq .SyncInfo

# Check database size and optimization
du -sh ~/.celestia-app/data/

# Look for faster sync times and reduced I/O wait
iostat -x 1
```

### Key Performance Indicators

- **Block processing time**: Should decrease significantly
- **I/O wait time**: Should be reduced
- **Memory usage**: May initially increase during lazy migration
- **Disk operations**: Should become more efficient over time

## Troubleshooting

### Common Issues

**Issue**: Node fails to start after upgrade
```bash
# Check logs for specific error
journalctl -u celestia-appd -n 100

# Common solution: Verify binary installation
celestia-appd version
```

**Issue**: State sync fails to connect
```bash
# Verify RPC endpoints are accessible
curl -s https://rpc-1.celestia.org/status

# Check trust height/hash are valid
curl -s https://rpc-1.celestia.org/block?height=TRUST_HEIGHT
```

**Issue**: Performance degradation during lazy migration
```bash
# Monitor migration progress in logs
grep -i "migration\|iavl" ~/.celestia-app/logs/celestia.log

# Consider switching to state sync if degradation is severe
```

### Recovery Procedures

**If you need to rollback:**

1. Stop celestia-app v4
2. Restore from backups:
   ```bash
   cp ~/validator_state_backup.json ~/.celestia-app/data/priv_validator_state.json
   cp ~/priv_validator_key_backup.json ~/.celestia-app/config/priv_validator_key.json
   cp ~/node_key_backup.json ~/.celestia-app/config/node_key.json
   ```
3. Reinstall previous celestia-app version
4. Restart node

## Best Practices

1. **Always backup validator keys** before any migration
2. **Test the migration process** on a non-validator node first
3. **Monitor performance metrics** before and after migration
4. **Plan for downtime** when doing state sync migration
5. **Keep backups** until you're confident the migration is successful
6. **Coordinate with your validator infrastructure** if running multiple nodes

## Additional Resources

- [Network Upgrades Documentation](adr-018-network-upgrades.md)
- [Celestia Node Documentation](https://docs.celestia.org/)
- [State Sync Configuration Guide](https://docs.tendermint.com/master/nodes/configuration.html#state-sync)

## Support

If you encounter issues during migration:

1. Check the [GitHub Issues](https://github.com/celestiaorg/celestia-app/issues) for known problems
2. Review node logs for specific error messages
3. Reach out on the [Celestia Discord](https://discord.gg/celestia) validator channels
4. File a new issue if you discover a bug in the migration process