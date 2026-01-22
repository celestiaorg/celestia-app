# Solutions Index

Searchable knowledge base for common problems and their solutions.

## Ante Handler

| Problem | Symptoms | Solution |
|---------|----------|----------|
| [Protocol-Injected Tx Handling](./ante-handler/protocol-injected-tx-handling.md) | Complex wrappers to skip decorators for unsigned txs | Single terminating decorator pattern |

## How to Use

**Search by error message:**
```bash
grep -r "error message" docs/solutions/
```

**Browse by category:**
- `docs/solutions/ante-handler/` - Ante handler patterns
- `docs/solutions/keeper-bugs/` - Keeper implementation issues
- `docs/solutions/proto-issues/` - Protobuf/gRPC problems
- `docs/solutions/test-failures/` - Test-related issues

**Find by tags:**
```bash
grep -l "tags:.*cosmos-sdk" docs/solutions/**/*.md
```
