## Signature Replay Bridge Challenge

This challenge demonstrates a signature replay vulnerability in cross-chain bridge contracts.

### Setup

1. Start by pressing the Start button
2. Connect to the RPC URLs for both chains
3. You will receive the same private key for both chains
4. Bridge contracts are deployed on both Chain 1 and Chain 2

### The Vulnerability

The bridge contract uses signatures to authorize withdrawals, but does NOT include the chainId in the signed message. This means a signature created for Chain 1 can be replayed on Chain 2.

### Your Goal

1. Get a valid withdrawal signature for Chain 1
2. Use the SAME signature to withdraw from Chain 2
3. The signature should work on both chains due to missing chainId in the message hash

### Technical Details

The vulnerable signature scheme:
```solidity
bytes32 messageHash = keccak256(abi.encodePacked(amount, recipient));
bytes32 ethSignedMessageHash = keccak256(abi.encodePacked("\x19Ethereum Signed Message:\n32", messageHash));
```

The fix would include chainId:
```solidity
bytes32 messageHash = keccak256(abi.encodePacked(amount, recipient, block.chainid));
```

### Winning Condition

Successfully withdraw tokens from BOTH chains using the same signature.
