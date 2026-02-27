import json
import os
from typing import Dict, Optional
from web3 import Web3

CONTRACT_ADDRESS_KEY = "OWNABLE_CONTRACT_ADDRESS"


def get_rpc_url() -> str:
    anvil_endpoint = os.environ.get("ANVIL_ENDPOINT", "http://localhost:8545")
    return anvil_endpoint


def compile_contract() -> tuple:
    with open("Ownable.sol", "r") as f:
        source = f.read()

    from solcx import compile_source

    compiled = compile_source(source, output_values=["abi", "bin"])
    contract_id = list(compiled.keys())[0]
    contract = compiled[contract_id]
    return contract["abi"], contract["bin"]


def deploy_contract(w3, abi, bytecode, account):
    contract_factory = w3.eth.contract(abi=abi, bytecode=bytecode)
    tx = contract_factory.constructor().build_transaction(
        {
            "from": account.address,
            "nonce": w3.eth.get_transaction_count(account.address),
            "gas": 2000000,
            "gasPrice": w3.eth.gas_price,
            "value": w3.to_wei(1, "ether"),
        }
    )
    signed = account.sign_transaction(tx)
    tx_hash = w3.eth.send_raw_transaction(signed.rawTransaction)
    receipt = w3.eth.wait_for_transaction_receipt(tx_hash)
    return w3.eth.contract(address=receipt.contractAddress, abi=abi)


def start() -> Dict:
    w3 = Web3(Web3.HTTPProvider(get_rpc_url()))

    private_key = os.environ.get(
        "PLAYER_PRIVATE_KEY",
        "0xb8c57cf2245c23279965b2e833a099a38d4fbd30fbfd8605d39eb502b8159e7d",
    )
    account = w3.eth.account.from_key(private_key)

    abi, bytecode = compile_contract()
    contract = deploy_contract(w3, abi, bytecode, account)

    result = {"anvilconfig": {"contract_address": contract.address}}
    print(json.dumps(result))
    return result


def is_solved() -> bool:
    w3 = Web3(Web3.HTTPProvider(get_rpc_url()))

    contract_address = os.environ.get(CONTRACT_ADDRESS_KEY)
    if not contract_address:
        return False

    abi, _ = compile_contract()
    contract = w3.eth.contract(address=contract_address, abi=abi)

    try:
        return contract.functions.isSolved().call()
    except Exception:
        return False


def genesis() -> Optional[Dict]:
    return None


def on_new_block():
    pass


def on_incoming_tx() -> tuple:
    return 200, ""


def on_tx():
    pass
