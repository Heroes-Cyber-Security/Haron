# ruff: noqa: E402
from gevent import monkey

monkey.patch_all()
import ast
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
import textwrap
import threading
import traceback
import urllib.request
import uuid
import venv
import zipfile
from contextlib import contextmanager, redirect_stderr
from typing import Callable, Dict, List, Tuple, Union
from urllib.parse import urlparse

import gevent
from bottle import HTTPError, get, post, request, run
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
ORCHESTRATOR_BASE = os.environ.get("ORCHESTRATOR_BASE", "http://orchestrator:8080")

jobs: Dict[str, "Job"] = {}
active_jobs = set()
prepared = set()
_instance_secrets: Dict[str, dict] = {}
# One imported chal module per instance. The event listener, `/validate`, and the
# hook routes must share it: a firewall whose rules live in module globals only
# works if everyone talks to the same module object.
_hook_modules: Dict[Tuple[str, str], object] = {}
# Hook callbacks read their instance data from the environment, like `start()`
# does. Serialise that window so two instances cannot observe each other's keys.
_environment_lock = threading.Lock()

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


def _instance_environment(pea_id: str) -> dict:
    """Environment a challenge sees when its hooks run.

    Mirrors the variables `/delegate` exports to `start()`, restricted to the
    ones derivable from the pea id. The deployer key is deliberately absent:
    it is generated per job and only exists once `start()` runs.
    """
    private_key, setup_address = generate_key_from_id(pea_id)
    return {
        "PEA_ID": pea_id,
        "PLAYER_PRIVATE_KEY": private_key,
        "SETUP_ADDRESS": setup_address,
    }


@contextmanager
def instance_environment(pea_id: str, extra: dict = None):
    """Applies the instance environment for the duration of a hook call.

    `extra` carries the per-job secrets (deployer key, endpoints) when they are
    known; hooks that run before `/delegate` only get what the pea id implies.
    """
    with _environment_lock:
        original = os.environ.copy()
        os.environ.update(_instance_environment(pea_id))
        if extra:
            os.environ.update({key: value for key, value in extra.items() if value is not None})
        try:
            yield
        finally:
            os.environ.clear()
            os.environ.update(original)


def _source_is_stub(source: str) -> bool:
    """Whether a hook body is only a docstring plus an empty return."""
    try:
        node = ast.parse(textwrap.dedent(source)).body[0]
    except (SyntaxError, IndexError):
        return False

    if not isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
        return False

    for statement in node.body:
        if isinstance(statement, ast.Pass):
            continue
        if (
            isinstance(statement, ast.Expr)
            and isinstance(statement.value, ast.Constant)
            and isinstance(statement.value.value, str)
        ):
            continue
        if isinstance(statement, ast.Return):
            value = statement.value
            if value is None or isinstance(value, ast.Constant):
                continue
            if isinstance(value, ast.Tuple) and all(
                isinstance(element, ast.Constant) for element in value.elts
            ):
                continue
            if isinstance(value, ast.Dict) and not value.keys:
                continue
            if (
                isinstance(value, ast.Call)
                and isinstance(value.func, ast.Name)
                and value.func.id == "dict"
                and not value.args
                and not value.keywords
            ):
                continue
            return False
        return False

    return True


def _function_is_stub(fn) -> bool:
    """Whether a hook body is only a docstring plus an empty return.

    Challenge templates ship every hook as a stub. Treating those as "not
    implemented" keeps `/hooks` and the firewall off until an author actually
    writes something, without forcing them to delete the template.
    """
    try:
        source = inspect.getsource(fn)
    except (OSError, TypeError):
        return False
    return _source_is_stub(source)


def ensure_instance_source(h: str, pea_id: str) -> str:
    """Makes sure the instance's challenge source is on disk.

    Unlike `prepare_instance`, this never builds the challenge venv. Hook code
    runs in the worker process, so importing `chal.py` only needs the source;
    building a venv here would blow the orchestrator's deploy-time timeout.
    """
    work_dir = instance_dir(h, pea_id)
    os.makedirs(work_dir, exist_ok=True)

    if os.path.exists(os.path.join(work_dir, "chal.py")):
        return work_dir

    zip_path = challenge_zip_path(h)
    if not os.path.exists(zip_path):
        zip_path = os.path.join(challenge_template_dir(h), f"{h}.zip")
    if not os.path.exists(zip_path):
        raise FileNotFoundError(f"challenge package missing for {h}")

    with zipfile.ZipFile(zip_path, "r") as zr:
        for member in zr.namelist():
            if member.split("/", 1)[0] == ".venv":
                continue
            zr.extract(member, work_dir)

    return work_dir


def load_hook_module(h: str, pea_id: str, extra_env: dict = None):
    """Imports (once) and returns the instance's chal module.

    `/start`, `/validate`, and the hook routes all go through here so that
    module-level state is shared, which is what a stateful firewall needs.
    """
    key = (h, pea_id)
    cached = _hook_modules.get(key)
    if cached is not None:
        return cached

    task_dir = ensure_instance_source(h, pea_id)
    module_name = f"chal_{h}_{pea_id}"
    module_path = os.path.join(task_dir, "chal.py")

    spec = importlib.util.spec_from_file_location(module_name, module_path)
    if spec is None or spec.loader is None:
        raise HTTPError(500, f"Unable to load chal module at {module_path}")

    module = importlib.util.module_from_spec(spec)
    sys.modules[module_name] = module

    with instance_environment(pea_id, extra_env):
        spec.loader.exec_module(module)

    _hook_modules[key] = module
    return module


def _call_with_chain(handler: Callable, chain_id: int, payload):
    """Dispatches a hook using the same 0/1/2-argument convention as on_new_block."""
    parameter_count = len(inspect.signature(handler).parameters)
    if parameter_count == 0:
        return handler()
    if parameter_count == 1:
        return handler(chain_id)
    return handler(chain_id, payload)


def _normalize_verdict(result) -> Tuple[int, str]:
    """Coerces an on_incoming_tx return value into `(status, message)`."""
    if isinstance(result, (tuple, list)):
        status = int(result[0]) if len(result) >= 1 else 200
        message = str(result[1]) if len(result) >= 2 and result[1] is not None else ""
        return status, message
    if isinstance(result, bool):
        return (200, "") if result else (403, "transaction rejected")
    if isinstance(result, int):
        return result, ""
    return 200, ""


def _checksum(value) -> str:
    try:
        return Web3.to_checksum_address(value)
    except Exception:
        return str(value)


def _hex(value) -> str:
    if isinstance(value, str):
        return value if value.startswith("0x") else "0x" + value
    return "0x" + bytes(value).hex()


def _decode_raw_transaction(raw: str) -> dict:
    """Best-effort decode of a serialised transaction for the firewall.

    `from` is always recovered. The remaining fields come from eth-account's
    decoders and are omitted when the transaction shape is unrecognised, so a
    hook can rely on `from` and treat everything else as optional.
    """
    from eth_account import Account

    data = bytes.fromhex(raw.removeprefix("0x").removeprefix("0X"))
    transaction = {"raw": "0x" + data.hex(), "hash": Web3.keccak(data).hex()}

    try:
        transaction["from"] = _checksum(Account.recover_transaction(data))
    except Exception:
        pass

    fields = None
    try:
        from eth_account._utils.typed_transactions import TypedTransaction

        fields = TypedTransaction.from_bytes(data).as_dict()
    except Exception:
        try:
            from eth_account._utils.legacy_transactions import Transaction

            fields = Transaction.from_bytes(data).as_dict()
        except Exception:
            fields = None

    if isinstance(fields, dict):
        for source, target in (
            ("to", "to"),
            ("value", "value"),
            ("nonce", "nonce"),
            ("gas", "gas"),
            ("data", "data"),
            ("chainId", "chain_id"),
            ("gasPrice", "gas_price"),
            ("maxFeePerGas", "max_fee_per_gas"),
            ("maxPriorityFeePerGas", "max_priority_fee_per_gas"),
        ):
            if source not in fields or fields[source] is None:
                continue
            value = fields[source]
            transaction[target] = _hex(value) if isinstance(value, (bytes, bytearray)) else value

        if transaction.get("to"):
            transaction["to"] = _checksum(transaction["to"])

    return transaction


def _incoming_transaction(payload: dict) -> dict:
    """Normalises the screening payload sent by the orchestrator."""
    request_tx = payload.get("tx")
    if isinstance(request_tx, dict):
        transaction = dict(request_tx)
        if transaction.get("from"):
            transaction["from"] = _checksum(transaction["from"])
        if transaction.get("to"):
            transaction["to"] = _checksum(transaction["to"])
        return transaction
    return _decode_raw_transaction(payload.get("raw") or "0x")


def _encode_precompile_result(result) -> str:
    if result is None:
        return "0x"
    if isinstance(result, (bytes, bytearray)):
        return "0x" + bytes(result).hex()
    if isinstance(result, str):
        return result if result.startswith("0x") else "0x" + result
    if isinstance(result, int):
        return "0x" + result.to_bytes(32, "big").hex()
    return "0x"


def _precompile_functions(module, chain_id: int) -> dict:
    """The address -> callable map for one chain, evaluated once per module."""
    normalized = getattr(module, "__har_precompiles__", None)
    if normalized is None:
        normalized = {}
        precompile_fn = getattr(module, "precompile", None)
        if callable(precompile_fn) and not _function_is_stub(precompile_fn):
            try:
                mapping = precompile_fn()
            except Exception as exc:
                print(f"hooks: precompile() failed: {exc}", file=sys.stderr)
                mapping = None
            if isinstance(mapping, dict):
                for cid, functions in mapping.items():
                    if not isinstance(functions, dict):
                        continue
                    try:
                        key = int(cid)
                    except (TypeError, ValueError):
                        continue
                    normalized[key] = {
                        _checksum(address): function
                        for address, function in functions.items()
                        if callable(function)
                    }
        setattr(module, "__har_precompiles__", normalized)

    try:
        return normalized.get(int(chain_id), {})
    except (TypeError, ValueError):
        return {}


def arm_orchestrator(uid: str) -> None:
    """Tells the orchestrator the instance is deployed and the firewall may arm."""
    url = f"{ORCHESTRATOR_BASE}/arm/{uid}"
    arm_request = urllib.request.Request(url, method="POST")
    try:
        with urllib.request.urlopen(arm_request, timeout=5) as response:
            print(f"hooks: armed firewall for {uid}: {response.status}", file=sys.stderr)
    except Exception as exc:
        print(f"hooks: unable to arm firewall for {uid}: {exc}", file=sys.stderr)


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

    def __init__(self, uid, task, anvil_endpoints, chain_ids):
        self.uid = uid
        self.task = task
        self.anvil_endpoints = anvil_endpoints
        self.chain_ids = chain_ids
        self.watchers: List = []
        self.module = None
        self.last_blocks: Dict[int, int] = {}

    def endpoint_for(self, chain_id: int) -> str:
        try:
            index = self.chain_ids.index(chain_id)
        except ValueError:
            return self.anvil_endpoints[0] if self.anvil_endpoints else ""
        return self.anvil_endpoints[index]

    def bind_handlers(self, chain_id: int, pea_id: str):
        """Loads the shared module and validates the block hooks."""
        # Shared per-instance module: the block watcher, `/validate`, and the
        # hook routes must all see the same module globals.
        module = load_hook_module(self.task, pea_id, _instance_secrets.get(self.uid))

        handler = getattr(module, "on_new_block", None)
        if not callable(handler):
            raise AttributeError("Expected callable 'on_new_block' in chal module")

        self.module = module

    def _block_data(self, block) -> Dict:
        return {
            "number": block["number"],
            "hash": _hex(block["hash"]),
            "parentHash": _hex(block["parentHash"]),
            "timestamp": block["timestamp"],
            "miner": _checksum(block.get("miner", "")),
            "gasLimit": block["gasLimit"],
            "gasUsed": block["gasUsed"],
            "baseFeePerGas": block.get("baseFeePerGas", 0),
        }

    def dispatch_block(self, chain_id: int, w3: Web3, number: int):
        """Runs `on_new_block` and then `on_tx` for one mined block."""
        try:
            block = w3.eth.get_block(number, full_transactions=True)
        except Exception as exc:
            print(
                f"watcher: failed to fetch block {number} on chain {chain_id}: {exc}",
                file=sys.stderr,
            )
            return

        block_data = self._block_data(block)

        on_new_block = getattr(self.module, "on_new_block", None)
        if callable(on_new_block):
            try:
                _call_with_chain(on_new_block, chain_id, block_data)
            except Exception as exc:
                print(f"on_new_block: hook failed on chain {chain_id}: {exc}", file=sys.stderr)

        on_tx = getattr(self.module, "on_tx", None)
        if not callable(on_tx) or _function_is_stub(on_tx):
            return

        # `on_tx` is sugar over `on_new_block`: the same block, one call per
        # successfully mined transaction.
        for transaction in block.get("transactions", []):
            if not isinstance(transaction, dict):
                continue
            try:
                receipt = w3.eth.get_transaction_receipt(transaction["hash"])
            except Exception:
                continue
            if receipt.get("status") != 1:
                continue

            payload = dict(transaction)
            payload["from"] = _checksum(payload.get("from", ""))
            if payload.get("to"):
                payload["to"] = _checksum(payload["to"])

            try:
                _call_with_chain(on_tx, chain_id, payload)
            except Exception as exc:
                print(
                    f"on_tx: hook failed for {payload.get('hash')} on chain {chain_id}: {exc}",
                    file=sys.stderr,
                )

    def watch(self, chain_id: int, interval: float = 1.0):
        """Polls the head and dispatches the block hooks.

        Polling keeps the worker free of the asyncio/gevent mixing that an
        external websocket listener would introduce, and gevent makes the
        blocking web3 calls cooperative across instances.
        """
        endpoint = self.endpoint_for(chain_id)
        if not endpoint:
            return

        w3 = Web3(Web3.HTTPProvider(endpoint))
        while True:
            try:
                number = w3.eth.block_number
            except Exception:
                gevent.sleep(interval)
                continue

            if number > self.last_blocks.get(chain_id, -1):
                # Dispatch every block we have not seen yet, so a slow poll
                # never skips a transaction.
                start = max(self.last_blocks.get(chain_id, -1) + 1, 0)
                self.last_blocks[chain_id] = number
                for pending in range(start, number + 1):
                    self.dispatch_block(chain_id, w3, pending)

            gevent.sleep(interval)

    def start_watching(self):
        if not self.chain_ids or self.watchers:
            return

        pea_id = extract_pea_id(self.anvil_endpoints[0])
        self.bind_handlers(self.chain_ids[0], pea_id)

        on_new_block = getattr(self.module, "on_new_block", None)
        on_tx = getattr(self.module, "on_tx", None)
        block_hook = callable(on_new_block) and not _function_is_stub(on_new_block)
        tx_hook = callable(on_tx) and not _function_is_stub(on_tx)

        # Nothing to dispatch: do not poll a node for a challenge that only
        # ships the template stubs.
        if not block_hook and not tx_hook:
            return

        for chain_id in self.chain_ids:
            self.watchers.append(gevent.spawn(self.watch, chain_id))

    def stop(self):
        for watcher in self.watchers:
            try:
                watcher.kill()
            except Exception:
                pass
        self.watchers.clear()

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
        pea_id = extract_pea_id(job.anvil_endpoints[0]) if job.anvil_endpoints else ""
        _hook_modules.pop((job.task, pea_id), None)
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

    # `start()` has deployed the challenge, so the setup transactions are done.
    # Only now may the firewall screen anything, otherwise it would drop the
    # challenge's own deployments and funding.
    arm_orchestrator(uid)

    # Block hooks start last: they only make sense once the instance exists.
    try:
        jobs[uid].start_watching()
    except Exception as exc:
        print(f"hooks: unable to start block watcher for {uid}: {exc}", file=sys.stderr)

    job_dict = jobs[uid].to_dict()
    return job_dict


@post("/start/:uid")
def start_job(uid):
    if uid not in jobs:
        raise HTTPError(404, "Job not found")
    job = jobs[uid]
    active_jobs.add(job)
    job.start_watching()
    return "OK"


@get("/hooks/:h")
def hooks_metadata(h):
    """Deploy-time hook metadata: genesis documents and precompile addresses.

    Called by the orchestrator before any node exists, so only data that can be
    derived from the pea id is available. The callables stay in this process.
    """
    safe_hash = sanitize_challenge_hash(h)
    pea_id = request.query.get("pea_id") or ""
    chain_ids = [int(part) for part in request.query.get("chains", "").split(",") if part.strip()]

    try:
        module = load_hook_module(safe_hash, pea_id)
    except Exception as exc:
        print(f"hooks: unable to load {safe_hash} for {pea_id}: {exc}", file=sys.stderr)
        return {}

    result: Dict = {"genesis": {}, "precompiles": {}, "incoming_tx": False}

    with instance_environment(pea_id):
        genesis_fn = getattr(module, "genesis", None)
        if callable(genesis_fn) and not _function_is_stub(genesis_fn):
            try:
                documents = genesis_fn()
            except Exception as exc:
                print(f"hooks: genesis() failed for {safe_hash}: {exc}", file=sys.stderr)
                documents = None
            if isinstance(documents, dict):
                for chain, document in documents.items():
                    if isinstance(document, dict):
                        result["genesis"][str(chain)] = document

        for chain_id in chain_ids:
            functions = _precompile_functions(module, chain_id)
            if functions:
                result["precompiles"][str(chain_id)] = list(functions)

    incoming_tx = getattr(module, "on_incoming_tx", None)
    result["incoming_tx"] = callable(incoming_tx) and not _function_is_stub(incoming_tx)

    return result


@post("/hooks/:h/precompile")
def hooks_precompile(h):
    """Runs one Python-backed precompile call on behalf of the orchestrator."""
    safe_hash = sanitize_challenge_hash(h)
    payload = request.json or {}

    try:
        module = load_hook_module(safe_hash, payload.get("pea_id", ""))
    except Exception as exc:
        print(f"hooks: unable to load {safe_hash} for precompile: {exc}", file=sys.stderr)
        return {"status": "revert", "bytes": "0x"}

    chain_id = payload.get("chain_id")
    functions = _precompile_functions(module, chain_id)
    function = functions.get(_checksum(payload.get("address", "")))
    if function is None:
        return {"status": "revert", "bytes": "0x"}

    call = {
        "chain_id": chain_id,
        "address": _checksum(payload.get("address", "")),
        "caller": _checksum(payload.get("caller", "")),
        "value": payload.get("value", "0"),
        "data": payload.get("data", "0x"),
        "gas": payload.get("gas", 0),
        "is_static": bool(payload.get("is_static", False)),
    }

    try:
        with instance_environment(payload.get("pea_id", "")):
            result = function(call)
    except Exception as exc:
        print(f"hooks: precompile {call['address']} failed: {exc}", file=sys.stderr)
        return {"status": "revert", "bytes": "0x"}

    return {"status": "success", "bytes": _encode_precompile_result(result)}


@post("/hooks/:h/incoming_tx")
def hooks_incoming_tx(h):
    """Consults the challenge's transaction firewall.

    Any 4xx status tells the orchestrator to drop the transaction and relay
    `message` to the player.
    """
    safe_hash = sanitize_challenge_hash(h)
    payload = request.json or {}

    try:
        module = load_hook_module(safe_hash, payload.get("pea_id", ""))
    except Exception as exc:
        print(f"hooks: unable to load {safe_hash} for screening: {exc}", file=sys.stderr)
        return {"status": 200, "message": ""}

    handler = getattr(module, "on_incoming_tx", None)
    if not callable(handler) or _function_is_stub(handler):
        return {"status": 200, "message": ""}

    try:
        transaction = _incoming_transaction(payload)
        with instance_environment(payload.get("pea_id", "")):
            status, message = _normalize_verdict(
                _call_with_chain(handler, payload.get("chain_id"), transaction)
            )
    except Exception as exc:
        print(f"hooks: on_incoming_tx failed: {exc}", file=sys.stderr)
        return {"status": 200, "message": ""}

    return {"status": status, "message": message}


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

    # Same module the listener and the hook routes use, so a challenge can keep
    # state in module globals between on_incoming_tx and is_solved.
    try:
        module = load_hook_module(job.task, pea_id)
    except Exception as exc:
        print(f"validate: unable to load chal module: {exc}", file=sys.stderr)
        raise HTTPError(500, "Unable to load chal module")

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
        with redirect_stderr(f), instance_environment(pea_id, _instance_secrets.get(uid)):
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
