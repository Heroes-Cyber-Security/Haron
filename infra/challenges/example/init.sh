forge init --no-git --force .

# Cleanup
rm src/Counter.sol
rm test/Counter.t.sol
rm script/Counter.s.sol

# Setup.sol
cat <<EOF > src/Setup.sol
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Setup {
	constructor() {}

	function isSolved() public view returns (bool) {
		return true;
	}
}
EOF
