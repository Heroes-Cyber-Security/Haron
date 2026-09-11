# ruff: noqa: E402
from gevent import monkey

monkey.patch_all()
import hashlib
import importlib.util
import inspect
import io
import json
import os
import re
import secrets
import shutil
import subprocess
import sys
import traceback
import uuid
import venv
import zipfile
from contextlib import redirect_stderr
from typing import Callable, Dict, List, Union
from urllib.parse import urlparse

from bottle import HTTPError, get, post, request, run
from eth_listener import EthListener
from web3 import Web3

CHALLENGE_HASH_PATTERN = re.compile(r"^[a-zA-Z0-9_-]+$")


def sanitize_challenge_hash(h: str) -> str:
    """Validate and sanitize challenge hash to prevent path traversal."""
    safe_hash = os.path.basename(h)
    if ".." in safe_hash:
        raise HTTPError(400, "Invalid challenge hash: contains directory traversal")
    if not CHALLENGE_HASH_PATTERN.match(safe_hash):
        raise HTTPError(400, "Invalid challenge hash format")
    return safe_hash


BASE_CACHE_DIR = "/home/ctf/cache"
CONTRACT_ADDRESS_KEY = "OWNABLE_CONTRACT_ADDRESS"

jobs: Dict[str, "Job"] = {}
active_jobs = set()
prepared = set()
_instance_secrets: Dict[str, dict] = {}

def challenge_zip_path(h: str) -> str:
    """The uploaded challenge package, stored once per challenge hash."""
    return os.path.join(BASE_CACHE_DIR, h, f"{h}.zip")

def challenge_template_dir(h: str) -> str:
    """Per challenge directory holding the extracted source and the built venv."""
    return os.path.join(BASE_CACHE_DIR, h, "template")

def instance_dir(h: str, pea_id: str) -> str:
    """Per instance working directory holding its own copy of the source."""
    return os.path.join(BASE_CACHE_DIR, f"{h}_{pea_id}")

def shared_python(h: str) -> str:
    """Interpreter from the challenge venv.

    A venv is not relocatable, so instead of copying it per instance every
    instance runs the single venv built when the challenge was first prepared.
    """
    return os.path.join(challenge_template_dir(h), ".venv", "bin", "python")

def prepare_challenge(h: str) -> None:
    """Extract the package and build the venv once per challenge hash.

    The cached template is verified on disk, so a wiped cache or a replaced
    volume is rebuilt instead of leaving every instance pointing at a missing
    interpreter.
    """
    template_dir = challenge_template_dir(h)
    interpreter = os.path.join(template_dir, ".venv", "bin", "python")
    source = os.path.join(template_dir, "chal.py")

    if h in prepared and os.path.exists(interpreter) and os.path.exists(source):
        return

    prepared.discard(h)
    os.makedirs(template_dir, exist_ok=True)

    zip_path = os.path.join(template_dir, f"{h}.zip")
    base_zip_path = challenge_zip_path(h)
    if os.path.exists(base_zip_path):
        shutil.copy(base_zip_path, zip_path)

    with zipfile.ZipFile(zip_path, "r") as zr:
        zr.extractall(template_dir)

    venv_dir = os.path.join(template_dir, ".venv")
    venv.create(venv_dir, clear=True, with_pip=True, symlinks=True)
    pip_executable = os.path.join(venv_dir, "bin", "pip")
    requirements_path = os.path.join(template_dir, "requirements.txt")
    subprocess.run([pip_executable, "install", "-r", requirements_path], cwd=template_dir)

    prepared.add(h)

def prepare_instance(h: str, pea_id: str) -> str:
    """Give one instance its own copy of the challenge source.

    Preparation is per challenge, but the working directory is per instance, so
    the second player of a challenge gets the same treatment as the first.
    """
    prepare_challenge(h)

    work_dir = instance_dir(h, pea_id)
    os.makedirs(work_dir, exist_ok=True)

    if os.path.exists(os.path.join(work_dir, "chal.py")):
        return work_dir

    zip_path = challenge_zip_path(h)
    if not os.path.exists(zip_path):
        zip_path = os.path.join(challenge_template_dir(h), f"{h}.zip")

    with zipfile.ZipFile(zip_path, "r") as zr:
        for member in zr.namelist():
            # The venv belongs to the shared template, never to an instance.
            if member.split("/", 1)[0] == ".venv":
                continue
            zr.extract(member, work_dir)

    return work_dir


class InstanceDetail:
    def __init__(
        self,
        instance_id: str,
        challenge_hash: str,
        setup_address: str,
        player_address: str,
        player_private_key: str,
        anvil_endpoints: list[str],
        chain_ids: list[int],
        anvil_config: dict,
        cwd: str,
    ):
        self.instance_id = instance_id
        self.challenge_hash = challenge_hash
        self.setup_address = setup_address
        self.player_address = player_address
        self.player_private_key = player_private_key
        self.anvil_endpoints = anvil_endpoints
        self.chain_ids = chain_ids
        self.anvil_config = anvil_config
        self.cwd = cwd


class Report(object):
    def to_dict(self):
        return {"anvil_config": {}}


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
        for idx, (endpoint, chain_id) in enumerate(zip(self.anvil_endpoints, self.chain_ids)):
            self.bind_handlers(cwd, chain_id)
            eth_listener = EthListener(endpoint)
            eth_listener.on("newHeads", self.new_heads_handler)
            self.eth_listeners.append(eth_listener)

    def stop(self):
        for listener in self.eth_listeners:
            try:
                if hasattr(listener, "stop"):
                    listener.stop()
            except Exception:
                pass
        self.eth_listeners.clear()

    def to_dict(self):
        return {
            "uid": self.uid,
            "task": self.task,
            "anvil_endpoints": self.anvil_endpoints,
            "report": self.report,
        }


def generate_key_from_id(pea_id: str, salt: str = "harondynamicsalt2025") -> tuple[str, str]:
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
    """Extract the Pea ID from an anvil endpoint URL.

    The orchestrator serves /anvil/{pea_id} and /anvil/{pea_id}/{chain_id}, so
    the id is the segment after `anvil`. Taking the last segment instead would
    return the chain id, which makes every instance share one pea id and
    therefore one player key and one task directory.
    """
    parsed = urlparse(anvil_endpoint)
    path_parts = [part for part in parsed.path.strip("/").split("/") if part]

    if "anvil" in path_parts:
        index = path_parts.index("anvil")
        if index + 1 < len(path_parts):
            return path_parts[index + 1]

    return path_parts[-1] if path_parts else ""


def generate_report(cwd, challenge_hash) -> Union[dict, Report]:
    """
    Note to myself: an empty report does nothing, a non-empty report overwrites settings
    chal.py should print out report only
    """
    try:
        python_executable = shared_python(challenge_hash)
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
    if jobid in jobs:
        job = jobs[jobid]
        job.stop()
        active_jobs.discard(job)
        del jobs[jobid]
    _instance_secrets.pop(jobid, None)
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

    jobs[uid] = Job(uid, h, anvil_endpoints, chain_ids)

    try:
        task_dir = prepare_instance(h, pea_id)
    except Exception as e:
        print(f"ERROR: Challenge initialization failed: {e}", file=sys.stderr)
        print(f"ERROR: Traceback: {traceback.format_exc()}", file=sys.stderr)
        prepared.discard(h)
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
            print(f"Warning: failed to fund account on {endpoint}: {e}", file=sys.stderr)

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

    print(f"DEBUG: Calling chal.py with CHAIN_IDS={env['CHAIN_IDS']}", file=sys.stderr)

    python_executable = shared_python(h)
    script_path = os.path.join(task_dir, "chal.py")

    print(
        f"DEBUG: Executing [{python_executable}, {script_path}] in {task_dir}",
        file=sys.stderr,
    )

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

        content = result.stdout or ""
        print(f"chal.py stdout: {content}", file=sys.stderr)

        if result.stderr:
            print(f"chal.py stderr: {result.stderr}", file=sys.stderr)

        if result.returncode != 0:
            print(f"ERROR: chal.py exited with code {result.returncode}", file=sys.stderr)

            jobs[uid].report = {"anvil_config": {}}
        elif content.strip():
            try:
                jobs[uid].report = json.loads(content)
                print(
                    "DEBUG: Successfully parsed chal.py output as JSON",
                    file=sys.stderr,
                )

            except json.JSONDecodeError as e:
                print(
                    f"ERROR: Failed to parse chal.py output as JSON: {e}",
                    file=sys.stderr,
                )

                print(f"ERROR: Raw output was: {content}", file=sys.stderr)

                jobs[uid].report = {"anvil_config": {}}
        else:
            print("WARNING: chal.py produced no stdout", file=sys.stderr)

            jobs[uid].report = {"anvil_config": {}}
    except subprocess.TimeoutExpired:
        print("ERROR: chal.py execution timed out after 120 seconds", file=sys.stderr)

        jobs[uid].report = {"anvil_config": {}}
    except Exception as e:
        print(f"ERROR: chal.py execution failed with exception: {e}", file=sys.stderr)

        traceback.print_exc()
        jobs[uid].report = {"anvil_config": {}}

    if "anvil_config" not in jobs[uid].report:
        jobs[uid].report["anvil_config"] = {}

    chains = jobs[uid].report["anvil_config"].get("chains", [])
    print(f"DEBUG: Extracted chains from report: {chains}", file=sys.stderr)

    if chains:
        print(f"DEBUG: Found {len(chains)} chains in report", file=sys.stderr)

        jobs[uid].report["anvil_config"]["setup_address"] = chains[0]["setup_address"]
    else:
        print(
            f"DEBUG: No chains found in report, using fallback setup_address={setup_address}",
            file=sys.stderr,
        )
        jobs[uid].report["anvil_config"]["setup_address"] = setup_address

    jobs[uid].report["anvil_config"]["player_private_key"] = private_key
    print(f"Final report: {json.dumps(jobs[uid].report)}", file=sys.stderr)
    print(
        f"DEBUG: Final report has {len(jobs[uid].report['anvil_config'].get('chains', []))} chains",
        file=sys.stderr,
    )

    job_dict = jobs[uid].to_dict()
    return job_dict


@post("/start/:uid")
def start_job(uid):
    job = jobs[uid]
    pea_id = extract_pea_id(job.anvil_endpoints[0])
    task_dir = instance_dir(job.task, pea_id)

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
    safe_hash = sanitize_challenge_hash(h)
    cache_path = os.path.join(BASE_CACHE_DIR, safe_hash)
    os.makedirs(cache_path, exist_ok=True)
    upload = next(iter(request.files.values()), None)
    if upload is None:
        raise HTTPError(400, "No file uploaded")
    upload.save(os.path.join(cache_path, f"{safe_hash}.zip"), overwrite=True)
    return "OK"


@get("/validate/:uid")
def validate(uid):
    if uid not in jobs:
        raise HTTPError(404, "Job not found")

    job = jobs[uid]
    pea_id = extract_pea_id(job.anvil_endpoints[0])
    task_dir = instance_dir(job.task, pea_id)

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

    setup_address = job.report.get("anvil_config", {}).get("setup_address")
    player_private_key = job.report.get("anvil_config", {}).get("player_private_key")

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
        anvil_config=job.report.get("anvil_config", {}),
        cwd=task_dir,
    )

    try:
        f = io.StringIO()
        with redirect_stderr(f):
            solved = module.is_solved(instance)
        stderr_output = f.getvalue()
        if stderr_output:
            print(f"is_solved stderr: {stderr_output}", file=sys.stderr)
            sys.stderr.flush()
        return {"solved": solved}
    except Exception as e:
        return {"solved": False, "error": str(e)}


os.makedirs(BASE_CACHE_DIR, exist_ok=True)
run(host="0.0.0.0", server="gevent")
