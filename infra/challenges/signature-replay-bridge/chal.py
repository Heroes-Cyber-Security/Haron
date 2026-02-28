from typing import Dict, Optional, Tuple, List
import json
import os
import subprocess
import sys
from web3 import Web3


def log_debug(msg: str):
    """Print debug messages to stderr so they don't interfere with JSON output"""
    print(msg, file=sys.stderr)


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
    contract_file = os.path.basename(contract_path)
    artifact_path = f"out/{contract_file}/{contract_name}.json"

    with open(artifact_path, "r") as f:
        artifact = json.load(f)

    log_debug(f"Loading artifact from {artifact_path}")
    if "bytecode" not in artifact:
        log_debug(f"  ERROR: No bytecode in artifact")
        log_debug(f"  Available keys: {artifact.keys()}")
        raise ValueError("Invalid artifact format - missing bytecode")

    if "object" not in artifact["bytecode"]:
        log_debug(f"  ERROR: No bytecode object")
        raise ValueError("Invalid bytecode format")

    bytecode = artifact["bytecode"]["object"]
    abi = artifact["abi"]

    log_debug(f"  Bytecode length: {len(bytecode)} chars")

    if "abi" not in artifact:
        log_debug(f"  ERROR: No ABI in artifact")
        raise ValueError("Missing ABI")

    log_debug(f"  ABI entries: {len(artifact['abi'])}")

    log_debug(f"Deploying {contract_path}...")
    log_debug(f"  From: {account.address}")

    if constructor_args:
        contract_factory = w3.eth.contract(abi=abi, bytecode=bytecode)
        deploy_data = contract_factory.constructor(
            *constructor_args
        ).data_in_transaction
        log_debug(f"  Deploy data length: {len(deploy_data)} chars")
        tx = contract_factory.constructor(*constructor_args).build_transaction(
            {
                "from": account.address,
                "nonce": w3.eth.get_transaction_count(account.address),
            }
        )
    else:
        contract_factory = w3.eth.contract(abi=abi, bytecode=bytecode)
        deploy_data = contract_factory.constructor().data_in_transaction
        log_debug(f"  Deploy data length: {len(deploy_data)} chars")
        tx = contract_factory.constructor().build_transaction(
            {
                "from": account.address,
                "nonce": w3.eth.get_transaction_count(account.address),
            }
        )

    log_debug(f"  Nonce: {tx['nonce']}")
    log_debug(f"  Gas limit: {tx['gas']}")
    if "gasPrice" in tx:
        log_debug(f"  Gas price: {tx['gasPrice']} wei")

    try:
        signed = account.sign_transaction(tx)
        tx_hash = w3.eth.send_raw_transaction(signed.raw_transaction)
        log_debug(f"  tx_hash: {tx_hash.hex()}")
        receipt = w3.eth.wait_for_transaction_receipt(tx_hash, timeout=30)

        log_debug(f"  Receipt status: {receipt.status}")
        log_debug(f"  Gas used: {receipt.gasUsed}")
        log_debug(f"  Contract address: {receipt.contractAddress}")

        if receipt.status == 0:
            log_debug(f"  FAILED: Contract deployment reverted")
            return None

        log_debug(f"  SUCCESS: Deployed at {receipt.contractAddress}")
        return receipt.contractAddress
    except Exception as e:
        log_debug(f"  ERROR: {e}")
        raise


def start() -> Dict:
    endpoints = json.loads(os.environ["ANVIL_ENDPOINTS"])
    chain_ids = json.loads(os.environ["CHAIN_IDS"])
    deployer_key = os.environ["DEPLOYER_PRIVATE_KEY"]

    log_debug(f"Starting multi-chain deployment for {len(endpoints)} chains")
    log_debug(f"  Endpoints: {endpoints}")
    log_debug(f"  Chain IDs: {chain_ids}")

    chains = []

    for endpoint, chain_id in zip(endpoints, chain_ids):
        log_debug(f"\n{'=' * 60}")
        log_debug(f"Deploying to Chain {chain_id}: {endpoint}")
        log_debug(f"{'=' * 60}")

        w3 = Web3(Web3.HTTPProvider(endpoint))

        if not w3.is_connected():
            log_debug(f"  ERROR: Cannot connect to {endpoint}")
            chains.append(
                {
                    "chainId": chain_id,
                    "name": f"Chain {chain_id}",
                    "rpc": endpoint,
                    "setup_address": None,
                    "error": "Connection failed",
                }
            )
            continue

        block_num = w3.eth.block_number
        log_debug(f"  Connected! Block #{block_num}")

        deployer_account = w3.eth.account.from_key(deployer_key)
        deployer_balance = w3.eth.get_balance(deployer_account.address)
        log_debug(f"  Deployer: {deployer_account.address}")
        log_debug(f"  Deployer balance: {w3.from_wei(deployer_balance, 'ether')} ETH")

        setup_address = deploy_contract(
            w3, deployer_key, "src/Setup.sol", constructor_args=[]
        )

        chains.append(
            {
                "chainId": chain_id,
                "name": f"Chain {chain_id}",
                "rpc": endpoint,
                "setup_address": setup_address,
            }
        )

    result = {
        "anvilconfig": {
            "chains": chains,
            "player_private_key": os.environ["PLAYER_PRIVATE_KEY"],
        }
    }

    print(json.dumps(result))
    return result


def is_solved() -> bool:
    endpoints = json.loads(os.environ["ANVIL_ENDPOINTS"])
    chain_ids = json.loads(os.environ["CHAIN_IDS"])

    all_signatures = []

    for endpoint, chain_id in zip(endpoints, chain_ids):
        w3 = Web3(Web3.HTTPProvider(endpoint))

        with open("out/Setup.sol/Setup.json", "r") as f:
            artifact = json.load(f)

        setup_abi = artifact["abi"]

        cache_dir = f"/home/ctf/cache"
        challenge_dirs = [
            d
            for d in os.listdir(cache_dir)
            if os.path.isdir(os.path.join(cache_dir, d))
        ]

        setup_address = None
        for challenge_dir in challenge_dirs:
            try:
                report_path = os.path.join(cache_dir, challenge_dir, "report.json")
                if os.path.exists(report_path):
                    with open(report_path, "r") as f:
                        report = json.load(f)
                        chains = report.get("anvilconfig", {}).get("chains", [])
                        for chain in chains:
                            if chain.get("chainId") == chain_id:
                                setup_address = chain.get("setup_address")
                                break
            except:
                continue

        if not setup_address:
            continue

        setup_contract = w3.eth.contract(address=setup_address, abi=setup_abi)
        bridge_address = setup_contract.functions.BRIDGE().call()

        with open("out/Bridge.sol/SignatureReplayBridge.json", "r") as f:
            bridge_artifact = json.load(f)

        bridge_abi = bridge_artifact["abi"]
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
