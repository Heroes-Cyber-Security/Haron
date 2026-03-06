from typing import Dict, Optional, Tuple
import json
import os
import subprocess
import sys
from web3 import Web3


def log_debug(msg: str):
    """Print debug messages to stderr so they don't interfere with JSON output"""
    print(msg, file=sys.stderr)


def deploy_contract(w3, account, contract_name, constructor_args=None):
    """
    Deploy a contract using foundry build artifacts
    """
    subprocess.run(
        ["/home/ctf/.foundry/bin/forge", "build"],
        check=True,
        capture_output=True,
        cwd=os.path.dirname(os.path.abspath(__file__)),
    )

    artifact_path = f"out/{contract_name}.sol/{contract_name}.json"
    with open(artifact_path, "r") as f:
        artifact = json.load(f)

    bytecode = artifact["bytecode"]["object"]
    abi = artifact["abi"]

    contract_factory = w3.eth.contract(abi=abi, bytecode=bytecode)

    if constructor_args:
        tx = contract_factory.constructor(*constructor_args).build_transaction(
            {
                "from": account.address,
                "nonce": w3.eth.get_transaction_count(account.address),
            }
        )
    else:
        tx = contract_factory.constructor().build_transaction(
            {
                "from": account.address,
                "nonce": w3.eth.get_transaction_count(account.address),
            }
        )

    signed = account.sign_transaction(tx)
    tx_hash = w3.eth.send_raw_transaction(signed.raw_transaction)
    receipt = w3.eth.wait_for_transaction_receipt(tx_hash)

    return w3.eth.contract(address=receipt["contractAddress"], abi=abi)


def fund_account(w3, from_account, to_address, amount):
    """
    Fund an account with ETH
    """
    tx = {
        "from": from_account.address,
        "to": to_address,
        "value": amount,
        "nonce": w3.eth.get_transaction_count(from_account.address),
        "gas": 21000,
        "gasPrice": w3.eth.gas_price,
    }
    signed = from_account.sign_transaction(tx)
    tx_hash = w3.eth.send_raw_transaction(signed.raw_transaction)
    w3.eth.wait_for_transaction_receipt(tx_hash)


def start() -> Dict:
    """
    Deploy the Setup contract and return its address
    """
    anvil_endpoints = json.loads(os.environ["ANVIL_ENDPOINTS"])
    chain_ids = json.loads(os.environ["CHAIN_IDS"])
    player_private_key = os.environ["PLAYER_PRIVATE_KEY"]
    setup_address = os.environ["SETUP_ADDRESS"]
    deployer_private_key = os.environ["DEPLOYER_PRIVATE_KEY"]

    result_chains = []

    for idx, (endpoint, chain_id) in enumerate(zip(anvil_endpoints, chain_ids)):
        w3 = Web3(Web3.HTTPProvider(endpoint))
        player_account = w3.eth.account.from_key(player_private_key)
        deployer_account = w3.eth.account.from_key(deployer_private_key)

        fund_account(w3, deployer_account, player_account.address, 10 * 10**18)

        contract = deploy_contract(w3, deployer_account, "Setup")

        result_chains.append(
            {
                "chainId": chain_id,
                "rpc": endpoint,
                "setup_address": contract.address,
            }
        )

    result = {
        "anvil_config": {
            "setup_address": setup_address,
            "player_private_key": player_private_key,
            "chains": result_chains,
        }
    }
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


def is_solved(instance) -> bool:
    """
    Returns True if the challenge has been solved
    """
    return True


if __name__ == "__main__":
    start()
