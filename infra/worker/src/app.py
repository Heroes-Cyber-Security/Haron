from gevent import monkey

monkey.patch_all()
from copy import deepcopy
from typing import Callable, Dict, Union

from bottle import get, post, run, request, HTTPError
from eth_listener import EthListener

import hashlib
import importlib.util
import json
import os
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


class Report(object):
    def to_dict(self):
        return {"anvilconfig": {}}


class Job(object):
    uid: str
    task: str
    anvil_endpoint: str
    report: dict = {}

    eth_listener: EthListener
    new_heads_handler: Callable[..., None]

    def __init__(self, uid, task, anvil_endpoint):
        self.uid = uid
        self.task = task
        self.anvil_endpoint = anvil_endpoint

    def bind_handlers(self, cwd):
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

        self.new_heads_handler = handler

    async def start(self, cwd):
        self.bind_handlers(cwd)

        eth_listener = EthListener(self.anvil_endpoint)
        eth_listener.on("newHeads", self.new_heads_handler)

    def to_dict(self):
        return {
            "uid": self.uid,
            "task": self.task,
            "anvil_endpoint": self.anvil_endpoint,
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
    /delegate/:h?anvil_endpoint=
    """
    uid = str(uuid.uuid4())
    task_dir = os.path.join(BASE_CACHE_DIR, h)
    anvil_endpoint = request.query["anvil_endpoint"]

    jobs[uid] = Job(uid, h, anvil_endpoint)

    if h not in initialized:
        initialized.add(h)
        try:
            os.makedirs(task_dir, exist_ok=True)
            zip_path = os.path.join(task_dir, f"{h}.zip")
            with zipfile.ZipFile(zip_path, "r") as zr:
                zr.extractall(task_dir)
            venv_dir = os.path.join(task_dir, ".venv")
            venv.create(venv_dir, clear=True, with_pip=True, symlinks=True)
            pip_executable = os.path.join(venv_dir, "bin", "pip")
            requirements_path = os.path.join(task_dir, "requirements.txt")
            subprocess.run(
                [pip_executable, "install", "-r", requirements_path], cwd=task_dir
            )
        except Exception:
            initialized.remove(h)
            return dict()

    pea_id = extract_pea_id(anvil_endpoint)
    private_key, setup_address = generate_key_from_id(pea_id)

    print(f"Generated for {pea_id}: address={setup_address}")

    try:
        fund_account(anvil_endpoint, setup_address)
        print(f"Funded account {setup_address} successfully")
    except Exception as e:
        print(f"Warning: failed to fund account {setup_address}: {e}")

    env = os.environ.copy()
    env["PLAYER_PRIVATE_KEY"] = private_key
    env["SETUP_ADDRESS"] = setup_address

    python_executable = os.path.join(task_dir, ".venv", "bin", "python")
    script_path = os.path.join(task_dir, "chal.py")
    result = subprocess.run(
        [python_executable, script_path],
        cwd=task_dir,
        capture_output=True,
        text=True,
        env=env,
    )
    content = result.stdout or ""
    print(f"chal.py stdout: {content}")
    print(f"chal.py stderr: {result.stderr}")
    if content.strip():
        jobs[uid].report = json.loads(content)
    else:
        jobs[uid].report = {"anvilconfig": {}}

    if "anvilconfig" not in jobs[uid].report:
        jobs[uid].report["anvilconfig"] = {}
    jobs[uid].report["anvilconfig"]["setup_address"] = setup_address
    jobs[uid].report["anvilconfig"]["player_private_key"] = private_key
    print(f"Final report: {json.dumps(jobs[uid].report)}")

    job_dict = jobs[uid].to_dict()
    return job_dict


@post("/start/:uid")
def start_job(uid):
    job = jobs[uid]
    task_dir = os.path.join(BASE_CACHE_DIR, job.task)

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
    task_dir = os.path.join(BASE_CACHE_DIR, job.task)

    module_name = f"chal_{uid}"
    module_path = os.path.join(task_dir, "chal.py")

    spec = importlib.util.spec_from_file_location(module_name, module_path)
    if spec is None or spec.loader is None:
        raise HTTPError(500, "Unable to load chal module")

    module = importlib.util.module_from_spec(spec)
    sys.modules[module_name] = module
    spec.loader.exec_module(module)

    setup_address = job.report.get("anvilconfig", {}).get("setup_address")
    if not setup_address:
        return {"solved": False, "error": "Setup address not found"}

    os.environ[CONTRACT_ADDRESS_KEY] = setup_address
    os.environ["ANVIL_ENDPOINT"] = job.anvil_endpoint

    try:
        solved = module.is_solved()
        return {"solved": solved}
    except Exception as e:
        return {"solved": False, "error": str(e)}


os.makedirs(BASE_CACHE_DIR, exist_ok=True)
run(host="0.0.0.0", server="gevent")
