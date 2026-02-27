// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface IBridge {
    function token() external view returns (address);
    function authorizedSigner() external view returns (address);
    function withdraw(uint256 amount, address recipient, bytes calldata signature) external;
    function usedSignatures(bytes calldata signature) external view returns (bool);
}
