// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/token/ERC20/IERC20.sol";

interface IBridge {
    function withdraw(uint256 amount, address recipient, bytes calldata signature) external;
}

contract SignatureReplayBridge is IBridge {
    IERC20 public token;
    address public authorizedSigner;

    mapping(bytes => bool) public usedSignatures;

    event Withdrawal(address indexed recipient, uint256 amount, bytes signature);

    constructor(address _token, address _authorizedSigner) {
        token = IERC20(_token);
        authorizedSigner = _authorizedSigner;
    }

    function withdraw(uint256 amount, address recipient, bytes calldata signature) external {
        require(!usedSignatures[signature], "Signature already used");
        
        address signer = recoverSigner(amount, recipient, signature);
        require(signer == authorizedSigner, "Invalid signature");
        
        usedSignatures[signature] = true;
        
        require(token.transfer(recipient, amount), "Transfer failed");
        
        emit Withdrawal(recipient, amount, signature);
    }

    function recoverSigner(uint256 amount, address recipient, bytes calldata signature) internal pure returns (address) {
        bytes32 messageHash = keccak256(abi.encodePacked(amount, recipient));
        bytes32 ethSignedMessageHash = keccak256(abi.encodePacked("\x19Ethereum Signed Message:\n32", messageHash));
        
        bytes memory vrs = signature;
        require(vrs.length == 65, "Invalid signature length");
        
        bytes32 r;
        bytes32 s;
        uint8 v;
        assembly {
            r := mload(add(vrs, 32))
            s := mload(add(vrs, 64))
            v := byte(0, mload(add(vrs, 96)))
        }
        
        return ecrecover(ethSignedMessageHash, v, r, s);
    }

    function emergencyWithdraw() external {
        require(token.transfer(msg.sender, token.balanceOf(address(this))), "Emergency withdrawal failed");
    }
}
