from typing import Dict, Optional, Tuple, List
import json
import os
import subprocess
from web3 import Web3


def deploy_contract(
    w3: Web3, private_key: str, contract_path: str, constructor_args: List = None
) -> str:
    account = w3.eth.account.from_key(private_key)

    subprocess.run(
        ["/home/ctf/.foundry/bin/forge", "build"],
        cwd=os.path.dirname(os.path.abspath(__file__)),
        check=True,
        capture_output=True,
    )

    contract_name = os.path.basename(contract_path).replace(".sol", "")
    artifact_path = f"out/{contract_path}/{contract_name}.json"

    with open(artifact_path, "r") as f:
        artifact = json.load(f)

    bytecode = artifact["deployedBytecode"]["object"]
    abi = artifact["abi"]

    if constructor_args:
        contract_factory = w3.eth.contract(abi=abi, bytecode=bytecode)
        tx = contract_factory.constructor(*constructor_args).build_transaction(
            {
                "from": account.address,
                "nonce": w3.eth.get_transaction_count(account.address),
                "gas": 2000000,
                "gasPrice": w3.eth.gas_price,
            }
        )
    else:
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
    tx_hash = w3.eth.send_raw_transaction(signed.rawTransaction)
    receipt = w3.eth.wait_for_transaction_receipt(tx_hash)

    return receipt.contractAddress


def start() -> Dict:
    endpoints = json.loads(os.environ["ANVIL_ENDPOINTS"])
    chain_ids = json.loads(os.environ["CHAIN_IDS"])
    private_key = os.environ["PLAYER_PRIVATE_KEY"]

    contracts = []

    for endpoint, chain_id in zip(endpoints, chain_ids):
        w3 = Web3(Web3.HTTPProvider(endpoint))

        token_address = deploy_contract(
            w3, private_key, "src/Token.sol", constructor_args=[1000000]
        )

        bridge_address = deploy_contract(
            w3,
            private_key,
            "src/Bridge.sol",
            constructor_args=[
                token_address,
                w3.eth.account.from_key(private_key).address,
            ],
        )

        contracts.append(
            {
                "rpc": endpoint,
                "chainId": chain_id,
                "bridge": bridge_address,
                "token": token_address,
            }
        )

    result = {"anvilconfig": {"chains": contracts, "player_private_key": private_key}}

    print(json.dumps(result))
    return result


def is_solved() -> bool:
    endpoints = json.loads(os.environ["ANVIL_ENDPOINTS"])
    chain_ids = json.loads(os.environ["CHAIN_IDS"])

    all_signatures = []

    for endpoint, chain_id in zip(endpoints, chain_ids):
        w3 = Web3(Web3.HTTPProvider(endpoint))

        with open("out/src/Bridge.sol/SignatureReplayBridge.json", "r") as f:
            artifact = json.load(f)

        bridge_abi = artifact["abi"]

        with open("out/src/Bridge.sol/SignatureReplayBridge.json", "r") as f:
            artifact = json.load(f)

        bridge_address_file = f"out/src/Bridge.sol/SignatureReplayBridge.json"

        import glob
        import os.path

        cache_dir = f"/home/ctf/cache"
        challenge_dirs = [
            d
            for d in os.listdir(cache_dir)
            if os.path.isdir(os.path.join(cache_dir, d))
        ]

        bridge_address = None
        for challenge_dir in challenge_dirs:
            try:
                report_path = os.path.join(cache_dir, challenge_dir, "report.json")
                if os.path.exists(report_path):
                    with open(report_path, "r") as f:
                        report = json.load(f)
                        chains = report.get("anvilconfig", {}).get("chains", [])
                        for chain in chains:
                            if chain.get("chainId") == chain_id:
                                bridge_address = chain.get("bridge")
                                break
            except:
                continue

        if not bridge_address:
            continue

        bridge_contract = w3.eth.contract(address=bridge_address, abi=bridge_abi)

        withdrawal_events = bridge_contract.events.Withdrawal.get_logs()

        for event in withdrawal_events:
            signature = event["args"]["signature"]
            all_signatures.append((chain_id, signature.hex()))

    signature_map = {}
    for chain_id, sig_hex in all_signatures:
        if sig_hex not in signature_map:
            signature_map[sig_hex] = []
        signature_map[sig_hex].append(chain_id)

    for sig_hex, chain_list in signature_map.items():
        if len(set(chain_list)) > 1:
            return True

    return False


def on_new_block(chain_id: int, block_data: Dict) -> None:
    pass


if __name__ == "__main__":
    start()
