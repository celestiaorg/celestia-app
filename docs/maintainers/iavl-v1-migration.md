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

Simply follow the upgrade instructions and normal safety precautions when restarting.

### State Sync Migration Process

Perform a fresh state sync with celestia-app v4 to ensure all state data uses the new IAVL v1 key layout from the start. Since operators already know how to perform state sync, simply backup your validator keys and state, then perform state sync with celestia-app v4.

