import zipfile
from gevent import monkey; monkey.patch_all()
from bottle import get, post, run, request, HTTPError
import subprocess, os, uuid

jobs = {}
initialized = set()

class Job(object):
	uid: str
	task: str
	anvil_endpoint: str

	def __init__(self, uid, task, anvil_endpoint):
		self.uid = uid
		self.task = task
		self.anvil_endpoint = anvil_endpoint

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
	cwd = f"/home/ctf/cache/{h}/"

	jobs[uid] = Job(uid, h, request.query['anvil_endpoint'])
	
	if h not in initialized:
		with zipfile.ZipFile(cwd + h + ".zip", "r") as zr:
			zr.extractall(cwd)
		subprocess.run(f"pip install -r {cwd}requirements.txt", shell=True, cwd=cwd)
		initialized.add(h)

	return uid

@get("/package/:h")
def package(h):
	cachePath = f"/home/ctf/cache/{h}/"
	if not os.path.exists(cachePath):
		os.mkdir(cachePath)
		return "false"
	return "true"

@post("/package/:h")
def package_post(h):
	cachePath = f"/home/ctf/cache/{h}/"
	os.makedirs(cachePath, exist_ok=True)
	upload = next(iter(request.files.values()), None)
	if upload is None:
		raise HTTPError(400, "No file uploaded")
	upload.save(os.path.join(cachePath, f"{h}.zip"), overwrite=True)
	return "OK"

if not os.path.exists("cache"):
	os.mkdir("cache")
run(host="0.0.0.0", server='gevent')
