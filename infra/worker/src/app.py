from gevent import monkey

monkey.patch_all()
from copy import deepcopy
from typing import Callable, Dict, Union, List

from bottle import get, post, run, request, HTTPError
from eth_listener import EthListener

import hashlib
import importlib.util
import json
import os
import secrets
import subprocess
import sys
import uuid
import venv
import zipfile
from urllib.parse import urlparse

from web3 import Web3

BASE_CACHE_DIR = "/home/ctf/cache"
CONTRACT_ADDRESS_KEY = "OWNABLE_CONTRACT_ADDRESS"

jobs: Dict[str, "Job"] = {}
active_jobs = set()
initialized = set()
_instance_secrets: Dict[str, dict] = {}


class InstanceDetail:
    """Represents challenge instance details for isolated solve verification"""

    instance_id: str
    challenge_hash: str
    setup_address: str
    player_address: str
    player_private_key: str
    anvil_endpoints: list[str]
    chain_ids: list[int]

    def __init__(
        self,
        instance_id: str,
        challenge_hash: str,
        setup_address: str,
        player_address: str,
        player_private_key: str,
        anvil_endpoints: list[str],
        chain_ids: list[int],
    ):
        self.instance_id = instance_id
        self.challenge_hash = challenge_hash
        self.setup_address = setup_address
        self.player_address = player_address
        self.player_private_key = player_private_key
        self.anvil_endpoints = anvil_endpoints
        self.chain_ids = chain_ids


class Report(object):
    def to_dict(self):
        return {"anvilconfig": {}}


class Job(object):
    uid: str
    task: str
    anvil_endpoints: List[str]
    chain_ids: List[int]
    report: dict = {}

    eth_listeners: List[EthListener]
    new_heads_handler: Callable[..., None]

    def __init__(self, uid, task, anvil_endpoints, chain_ids):
        self.uid = uid
        self.task = task
        self.anvil_endpoints = anvil_endpoints
        self.chain_ids = chain_ids
        self.eth_listeners = []

    def bind_handlers(self, cwd, chain_id: int):
        module_name = f"chal_{self.uid}"
        module_path = os.path.join(cwd, "chal.py")

        spec = importlib.util.spec_from_file_location(module_name, module_path)
        if spec is None or spec.loader is None:
            raise ImportError(f"Unable to load chal module at {module_path}")

        module = importlib.util.module_from_spec(spec)
        sys.modules[module_name] = module
        spec.loader.exec_module(module)

        handler = getattr(module, "on_new_block", None)
        if not callable(handler):
            raise AttributeError("Expected callable 'on_new_block' in chal module")

        import inspect

        sig = inspect.signature(handler)
        param_count = len(sig.parameters)

        if param_count == 0:

            def wrapped_handler(event, cid=chain_id):
                return handler()

            self.new_heads_handler = wrapped_handler
        elif param_count >= 2:

            def wrapped_handler(event, cid=chain_id):
                block_data = {
                    "number": event.number,
                    "hash": event.hash,
                    "parentHash": event.parent_hash,
                    "timestamp": event.timestamp,
                    "miner": event.miner,
                    "gasLimit": event.gas_limit,
                    "gasUsed": event.gas_used,
                    "baseFeePerGas": event.base_fee_per_gas,
                }
                return handler(cid, block_data)

            self.new_heads_handler = wrapped_handler
        else:

            def wrapped_handler(event, cid=chain_id):
                return handler(cid)

            self.new_heads_handler = wrapped_handler

    async def start(self, cwd):
        for idx, (endpoint, chain_id) in enumerate(
            zip(self.anvil_endpoints, self.chain_ids)
        ):
            self.bind_handlers(cwd, chain_id)
            eth_listener = EthListener(endpoint)
            eth_listener.on("newHeads", self.new_heads_handler)
            self.eth_listeners.append(eth_listener)

    def to_dict(self):
        return {
            "uid": self.uid,
            "task": self.task,
            "anvil_endpoints": self.anvil_endpoints,
            "report": self.report,
        }


def generate_key_from_id(
    pea_id: str, salt: str = "harondynamicsalt2025"
) -> tuple[str, str]:
    """Generate deterministic private key and address from Pea ID"""
    seed = hashlib.sha256((pea_id + salt).encode()).digest()
    private_key = seed.hex()
    w3 = Web3()
    account = w3.eth.account.from_key(private_key)
    return private_key, account.address


def fund_account(anvil_endpoint: str, address: str, amount_ether: float = 10000):
    """Fund account via Anvil cheat code"""
    w3_anvil = Web3(Web3.HTTPProvider(anvil_endpoint))
    w3_anvil.provider.make_request(
        "anvil_setBalance", [address, hex(w3_anvil.to_wei(amount_ether, "ether"))]
    )


def extract_pea_id(anvil_endpoint: str) -> str:
    """Extract Pea ID from anvil endpoint URL"""
    parsed = urlparse(anvil_endpoint)
    path_parts = parsed.path.strip("/").split("/")
    return path_parts[-1] if path_parts else ""


def generate_report(cwd) -> Union[dict, Report]:
    """
    Note to myself: an empty report does nothing, a non-empty report overwrites settings
    chal.py should print out report only
    """
    try:
        python_executable = os.path.join(cwd, ".venv", "bin", "python")
        script_path = os.path.join(cwd, "chal.py")
        result = subprocess.run(
            [python_executable, script_path],
            cwd=cwd,
            capture_output=True,
            text=True,
        )
        content = result.stdout or ""
        if not content.strip():
            return Report()

        return json.loads(content)
    except Exception:
        return Report()


@post("/stop/:jobid")
def stop(jobid):
    del jobs[jobid]
    return "OK"


@post("/delegate/:h")
def delegate(h):
    """
    /delegate/:h?anvil_endpoints=["http://...", "http://..."]
    """
    uid = str(uuid.uuid4())
    anvil_endpoints_str = request.query.get("anvil_endpoints")
    if anvil_endpoints_str:
        anvil_endpoints = json.loads(anvil_endpoints_str)
    else:
        anvil_endpoints = [request.query["anvil_endpoint"]]

    chain_ids = [int(ep.split("/")[-1]) for ep in anvil_endpoints]
    pea_id = extract_pea_id(anvil_endpoints[0])
    task_dir = os.path.join(BASE_CACHE_DIR, f"{h}_{pea_id}")

    jobs[uid] = Job(uid, h, anvil_endpoints, chain_ids)

    if h not in initialized:
        initialized.add(h)
        try:
            os.makedirs(task_dir, exist_ok=True)
            zip_path = os.path.join(task_dir, f"{h}.zip")

            base_zip_path = os.path.join(BASE_CACHE_DIR, h, f"{h}.zip")
            if os.path.exists(base_zip_path):
                import shutil

                shutil.copy(base_zip_path, zip_path)

            with zipfile.ZipFile(zip_path, "r") as zr:
                zr.extractall(task_dir)
            venv_dir = os.path.join(task_dir, ".venv")
            venv.create(venv_dir, clear=True, with_pip=True, symlinks=True)
            pip_executable = os.path.join(venv_dir, "bin", "pip")
            requirements_path = os.path.join(task_dir, "requirements.txt")
            subprocess.run(
                [pip_executable, "install", "-r", requirements_path], cwd=task_dir
            )
        except Exception as e:
            import traceback

            print(f"ERROR: Challenge initialization failed: {e}", file=sys.stderr)
            print(f"ERROR: Traceback: {traceback.format_exc()}", file=sys.stderr)
            initialized.remove(h)
            return dict()

    pea_id = extract_pea_id(anvil_endpoints[0])
    private_key, setup_address = generate_key_from_id(pea_id)

    print(f"Generated for {pea_id}: address={setup_address}", file=sys.stderr)

    deployer_key = "0x" + secrets.token_hex(32)
    w3 = Web3(Web3.HTTPProvider(anvil_endpoints[0]))
    deployer_account = w3.eth.account.from_key(deployer_key)
    deployer_address = deployer_account.address

    print(f"Generated deployer: address={deployer_address}", file=sys.stderr)

    for endpoint in anvil_endpoints:
        try:
            fund_account(endpoint, setup_address)
            fund_account(endpoint, deployer_address)
        except Exception as e:
            print(
                f"Warning: failed to fund account on {endpoint}: {e}", file=sys.stderr
            )

    env = os.environ.copy()
    env["PLAYER_PRIVATE_KEY"] = private_key
    env["SETUP_ADDRESS"] = setup_address
    env["DEPLOYER_PRIVATE_KEY"] = deployer_key
    env["DEPLOYER_ADDRESS"] = deployer_address
    env["ANVIL_ENDPOINTS"] = json.dumps(anvil_endpoints)
    env["CHAIN_IDS"] = json.dumps(chain_ids)

    _instance_secrets[uid] = {
        "PLAYER_PRIVATE_KEY": private_key,
        "SETUP_ADDRESS": setup_address,
        "DEPLOYER_PRIVATE_KEY": deployer_key,
        "DEPLOYER_ADDRESS": deployer_address,
        "ANVIL_ENDPOINTS": json.dumps(anvil_endpoints),
        "CHAIN_IDS": json.dumps(chain_ids),
    }

    print(
        f"DEBUG: Calling chal.py with ANVIL_ENDPOINTS={env['ANVIL_ENDPOINTS']}",
        file=sys.stderr,
    )
    sys.stdout.flush()
    print(f"DEBUG: Calling chal.py with CHAIN_IDS={env['CHAIN_IDS']}", file=sys.stderr)
    sys.stdout.flush()

    python_executable = os.path.join(task_dir, ".venv", "bin", "python")
    script_path = os.path.join(task_dir, "chal.py")

    print(
        f"DEBUG: Executing [{python_executable}, {script_path}] in {task_dir}",
        file=sys.stderr,
    )
    sys.stdout.flush()

    try:
        result = subprocess.run(
            [python_executable, script_path],
            cwd=task_dir,
            capture_output=True,
            text=True,
            env=env,
            timeout=120,
        )
        print(f"DEBUG: chal.py returncode={result.returncode}", file=sys.stderr)
        sys.stdout.flush()
        content = result.stdout or ""
        print(f"chal.py stdout: {content}", file=sys.stderr)
        sys.stdout.flush()
        if result.stderr:
            print(f"chal.py stderr: {result.stderr}", file=sys.stderr)
            sys.stdout.flush()

        if result.returncode != 0:
            print(
                f"ERROR: chal.py exited with code {result.returncode}", file=sys.stderr
            )
            sys.stdout.flush()
            jobs[uid].report = {"anvilconfig": {}}
        elif content.strip():
            try:
                jobs[uid].report = json.loads(content)
                print(
                    f"DEBUG: Successfully parsed chal.py output as JSON",
                    file=sys.stderr,
                )
                sys.stdout.flush()
            except json.JSONDecodeError as e:
                print(
                    f"ERROR: Failed to parse chal.py output as JSON: {e}",
                    file=sys.stderr,
                )
                sys.stdout.flush()
                print(f"ERROR: Raw output was: {content}", file=sys.stderr)
                sys.stdout.flush()
                jobs[uid].report = {"anvilconfig": {}}
        else:
            print(f"WARNING: chal.py produced no stdout", file=sys.stderr)
            sys.stdout.flush()
            jobs[uid].report = {"anvilconfig": {}}
    except subprocess.TimeoutExpired:
        print(f"ERROR: chal.py execution timed out after 120 seconds", file=sys.stderr)
        sys.stdout.flush()
        jobs[uid].report = {"anvilconfig": {}}
    except Exception as e:
        print(f"ERROR: chal.py execution failed with exception: {e}", file=sys.stderr)
        sys.stdout.flush()
        import traceback

        traceback.print_exc()
        jobs[uid].report = {"anvilconfig": {}}

    if "anvilconfig" not in jobs[uid].report:
        jobs[uid].report["anvilconfig"] = {}

    chains = jobs[uid].report["anvilconfig"].get("chains", [])
    print(f"DEBUG: Extracted chains from report: {chains}", file=sys.stderr)
    sys.stdout.flush()
    if chains:
        print(f"DEBUG: Found {len(chains)} chains in report", file=sys.stderr)
        sys.stdout.flush()
        jobs[uid].report["anvilconfig"]["setup_address"] = chains[0]["setup_address"]
    else:
        print(
            f"DEBUG: No chains found in report, using fallback setup_address={setup_address}",
            file=sys.stderr,
        )
        jobs[uid].report["anvilconfig"]["setup_address"] = setup_address

    jobs[uid].report["anvilconfig"]["player_private_key"] = private_key
    print(f"Final report: {json.dumps(jobs[uid].report)}", file=sys.stderr)
    print(
        f"DEBUG: Final report has {len(jobs[uid].report['anvilconfig'].get('chains', []))} chains",
        file=sys.stderr,
    )

    job_dict = jobs[uid].to_dict()
    return job_dict


@post("/start/:uid")
def start_job(uid):
    job = jobs[uid]
    pea_id = extract_pea_id(job.anvil_endpoints[0])
    task_dir = os.path.join(BASE_CACHE_DIR, f"{job.task}_{pea_id}")

    active_jobs.add(job)
    job.start(task_dir)


@get("/package/:h")
def package(h):
    cache_path = os.path.join(BASE_CACHE_DIR, h)
    if not os.path.exists(cache_path):
        os.makedirs(cache_path, exist_ok=True)
        return "false"
    return "true"


@post("/package/:h")
def package_post(h):
    cache_path = os.path.join(BASE_CACHE_DIR, h)
    os.makedirs(cache_path, exist_ok=True)
    upload = next(iter(request.files.values()), None)
    if upload is None:
        raise HTTPError(400, "No file uploaded")
    upload.save(os.path.join(cache_path, f"{h}.zip"), overwrite=True)
    return "OK"


@get("/validate/:uid")
def validate(uid):
    if uid not in jobs:
        raise HTTPError(404, "Job not found")

    job = jobs[uid]
    pea_id = extract_pea_id(job.anvil_endpoints[0])
    task_dir = os.path.join(BASE_CACHE_DIR, f"{job.task}_{pea_id}")

    module_name = f"chal_{uid}"
    module_path = os.path.join(task_dir, "chal.py")

    spec = importlib.util.spec_from_file_location(module_name, module_path)
    if spec is None or spec.loader is None:
        raise HTTPError(500, "Unable to load chal module")

    module = importlib.util.module_from_spec(spec)
    sys.modules[module_name] = module

    secrets = _instance_secrets.get(uid)
    original_env = os.environ.copy()
    if secrets:
        os.environ.update(secrets)

    spec.loader.exec_module(module)

    if secrets:
        os.environ.clear()
        os.environ.update(original_env)

    setup_address = job.report.get("anvilconfig", {}).get("setup_address")
    player_private_key = job.report.get("anvilconfig", {}).get("player_private_key")

    if not setup_address or not player_private_key:
        return {"solved": False, "error": "Instance details not found"}

    w3 = Web3(Web3.HTTPProvider(job.anvil_endpoints[0]))
    player_account = w3.eth.account.from_key(player_private_key)
    player_address = player_account.address

    instance = InstanceDetail(
        instance_id=pea_id,
        challenge_hash=job.task,
        setup_address=setup_address,
        player_address=player_address,
        player_private_key=player_private_key,
        anvil_endpoints=job.anvil_endpoints,
        chain_ids=job.chain_ids,
    )

    try:
        solved = module.is_solved(instance)
        return {"solved": solved}
    except Exception as e:
        return {"solved": False, "error": str(e)}


os.makedirs(BASE_CACHE_DIR, exist_ok=True)
run(host="0.0.0.0", server="gevent")
