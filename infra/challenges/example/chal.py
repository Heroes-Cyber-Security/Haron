import json
import os
import subprocess
import sys
from typing import Dict, Optional, Tuple

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
        tx = contract_factory.constructor(*constructor_args).build_transaction({
            "from": account.address,
            "nonce": w3.eth.get_transaction_count(account.address),
        })
    else:
        tx = contract_factory.constructor().build_transaction({
            "from": account.address,
            "nonce": w3.eth.get_transaction_count(account.address),
        })

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

        result_chains.append({
            "chainId": chain_id,
            "rpc": endpoint,
            "setup_address": contract.address,
        })

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
    EXPERIMENTAL: Python-backed EVM precompiles.

    Return `{chainId: {address: function}}`. Each function is installed as a
    real precompile on that chain, so it runs for internal CALLs as well as
    top-level eth_call. The function receives one dict:

        {
            "chain_id":  int,
            "address":   "0x...",   # the precompile address being called
            "caller":    "0x...",   # msg.sender of the call
            "value":     "0",       # call value as a decimal string
            "data":      "0x...",   # calldata
            "gas":       int,       # gas limit for the call
            "is_static": bool,      # whether this is a STATICCALL context
        }

    Return the output bytes (bytes, "0x..." string, or int). Raising an
    exception reverts the call. Do not call back into this chain from here: the
    orchestrator serialises requests to a node, so that would deadlock.
    """
    return dict()


def genesis() -> Optional[Dict]:
    """
    Return `{chainId: genesis.json contents}`.

    Each document is installed as that chain's genesis before the node starts,
    so it can pre-fund accounts, pre-deploy code, or set chain parameters.
    """
    return None


def on_new_block():
    """
    Triggered every time a new block has been mined.

    Accepts 0, 1, or 2 parameters: (), (chain_id), or (chain_id, block) where
    block carries number/hash/parentHash/timestamp/miner/gasLimit/gasUsed.
    """
    pass


def on_incoming_tx() -> Tuple[int, str]:
    """
    Triggered for every transaction a player submits, before it reaches the node.

    Accepts 0, 1, or 2 parameters: (), (chain_id), or (chain_id, tx) where tx
    carries at least `from`, plus `to`, `value`, `data`, `nonce`, `gas`,
    `gas_price`, and `hash` when they can be decoded.

    Returns:
     - int: HTTP status code; 4xx drops the transaction
     - str: Error message relayed to the player when the tx is dropped

    Note: this runs in the worker process, not in the `start()` subprocess, so
    module-level state set by `start()` is not visible here. Use the environment
    or recompute what you need.
    """
    return 200, ""


def on_tx():
    """
    Shorthand for on_new_block plus a per-transaction loop.

    Called once for every successfully mined transaction, with the same
    argument convention as on_incoming_tx.
    """
    pass


def is_solved(instance) -> bool:
    """
    Returns True if the challenge has been solved
    """
    return True


if __name__ == "__main__":
    start()
