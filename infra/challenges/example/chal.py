from typing import Dict, Optional, Tuple
import json
import os
import subprocess
from web3 import Web3


def start() -> Dict:
    """
    Deploy the Setup contract and return its address
    """
    anvil_endpoint = os.environ.get("ANVIL_ENDPOINT", "http://localhost:8545")
    private_key = os.environ["PLAYER_PRIVATE_KEY"]

    w3 = Web3(Web3.HTTPProvider(anvil_endpoint))
    account = w3.eth.account.from_key(private_key)

    # Build contract with forge
    subprocess.run(
        ["/home/ctf/.foundry/bin/forge", "build"], check=True, capture_output=True
    )

    # Read compiled artifact
    with open("out/Setup.sol/Setup.json", "r") as f:
        artifact = json.load(f)

    bytecode = artifact["deployedBytecode"]["object"]
    abi = artifact["abi"]

    # Deploy contract
    contract_factory = w3.eth.contract(abi=abi, bytecode=bytecode)
    tx = contract_factory.constructor().build_transaction(
        {
            "from": account.address,
            "nonce": w3.eth.get_transaction_count(account.address),
            "gas": 2000000,
            "gasPrice": w3.eth.gas_price,
        }
    )
    signed = account.sign_transaction(tx)
    tx_hash = w3.eth.send_raw_transaction(signed.raw_transaction)
    receipt = w3.eth.wait_for_transaction_receipt(tx_hash)

    result = {"anvilconfig": {"contract_address": receipt.contractAddress}}
    print(json.dumps(result))
    return result


def precompile() -> Dict:
    """
    EXPERIMENTAL
    """
    return dict()


def genesis() -> Optional[Dict]:
    """
    Returns a genesis.json file
    """
    return None


def on_new_block():
    """
    Triggered every time a new block has been mined
    """
    pass


def on_incoming_tx() -> Tuple[int, str]:
    """
    Triggered every time a new tx is proposed to the mempool

    Returns:
     - int: HTTP status code, with 4xx dropping the incoming tx
     - str: Error message when tx is dropped
    """
    return 200, ""


def on_tx():
    """
    Triggered every time a transaction has been included in a block
    """
    pass


def is_solved() -> bool:
    """
    Returns True if the challenge has been solved
    """
    return True


if __name__ == "__main__":
    start()
