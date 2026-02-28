// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "./Bridge.sol";
import "./Token.sol";

contract Setup {
    IBridge public BRIDGE;
    ReplayToken public TOKEN;

    constructor() {
        TOKEN = new ReplayToken(1000000);
        BRIDGE = new SignatureReplayBridge(address(TOKEN), msg.sender);
        TOKEN.mint(address(BRIDGE), 1000000 * 10 ** 18);
    }

    function getBridge() external view returns (address) {
        return address(BRIDGE);
    }

    function getToken() external view returns (address) {
        return address(TOKEN);
    }
}
