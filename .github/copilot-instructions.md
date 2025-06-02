# GitHub Copilot Instructions for Celestia App

These instructions guide GitHub Copilot to generate code that follows the coding standards and security practices of the Celestia App project.

## General Guidelines

Whenever you generate code or documentation:

1. Use extremely simple, direct language—no unnecessary adverbs.
2. Make the code self-explanatory. Only add comments when an implicit operation must be called out.
3. Follow instructions exactly. Think critically about security: always double-check for hidden bugs or vulnerabilities.
4. Produce code that is highly readable yet concise. Do not abstract prematurely; defer abstraction until it's truly needed.
5. When writing Go, adhere to the latest official Go best practices (idiomatic naming, error handling, package layout, etc.).

## Go-Specific Guidelines

### Error Handling
- Always handle errors explicitly using the established patterns in the codebase
- Use `errors.Wrap` or `errors.Wrapf` from `cosmossdk.io/errors` for error context
- Prefer specific error types from `sdkerrors` when appropriate
- Never ignore errors with blank identifiers unless absolutely necessary and well-documented

### Naming Conventions
- Use clear, descriptive variable and function names
- Follow Go naming conventions: `camelCase` for private, `PascalCase` for public
- Prefer `ctx` for context parameters
- Use `k` for keeper receivers in methods
- Use descriptive names for test cases and subtests

### Function Structure
- Keep functions focused and single-purpose
- Prefer early returns to reduce nesting
- Validate inputs at the beginning of functions
- Use guard clauses to handle edge cases early

### Testing
- Follow table-driven test patterns established in the codebase
- Use `testify/assert` and `testify/require` for assertions
- Name test cases descriptively to explain what is being tested
- Include both positive and negative test cases
- Test edge cases and error conditions

## Blockchain-Specific Guidelines

### Security Considerations
- Always validate user inputs, especially in message handlers
- Be cautious with arithmetic operations that could overflow
- Verify permissions and authority before state modifications
- Consider replay attacks and ensure proper nonce/sequence handling
- Validate cryptographic signatures and public keys
- Be mindful of gas consumption and potential DoS vectors

### Cosmos SDK Patterns
- Follow the established keeper pattern for module state management
- Use proper store prefixes and key construction patterns
- Implement proper protobuf message validation
- Follow ABCI method implementations (BeginBlock, EndBlock, etc.)
- Use appropriate store types (KVStore, Iterator patterns)

### Module Development
- Follow the module interface requirements
- Implement proper genesis import/export
- Use established parameter handling patterns
- Follow proper ante handler patterns for transaction processing
- Implement proper querier patterns for gRPC queries

## Celestia-Specific Guidelines

### Blob Handling
- When working with blob data, always validate namespace format and constraints
- Ensure proper square size calculations and validation
- Handle share commitment verification correctly
- Follow established patterns for blob inclusion proofs

### Data Availability
- Be mindful of data square layout constraints
- Properly handle namespace merkle tree operations
- Ensure proper share encoding and padding
- Follow established patterns for data root computation

### Consensus
- When modifying consensus-critical code, ensure deterministic behavior
- Be cautious with floating-point operations in consensus code
- Validate all inputs in PrepareProposal and ProcessProposal handlers
- Follow established patterns for transaction ordering and priority

## Code Organization

### Package Structure
- Follow the established directory structure
- Keep related functionality in appropriate packages
- Use internal packages for non-exported utilities
- Separate concerns between types, keeper, and client packages

### Imports
- Group imports logically: standard library, third-party, project-local
- Use specific imports rather than wildcard imports
- Avoid circular dependencies between packages

### Documentation
- Document all exported functions, types, and constants
- Use godoc-style comments that start with the item name
- Keep documentation concise but complete
- Document any non-obvious behavior or side effects

## Performance Considerations
- Be mindful of gas consumption in transaction processing
- Use efficient algorithms for cryptographic operations
- Consider caching for frequently accessed data
- Profile code when performance is critical
- Use appropriate data structures for the use case

## Dependencies
- Prefer standard library solutions when possible
- Use established Cosmos SDK patterns and utilities
- Avoid introducing unnecessary external dependencies
- Keep dependency versions aligned with the project requirements