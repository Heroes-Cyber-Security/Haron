## Vulnerable Ownable

Exploit the ownership transfer vulnerability to drain the contract.

### Objective
- The contract holds 1 ETH
- Drain all funds by becoming the owner
- Contract balance must reach 0 to solve

### Hints
1. Review the `transferOwnership` function carefully
2. Anyone can call certain functions
3. Only the owner can withdraw funds