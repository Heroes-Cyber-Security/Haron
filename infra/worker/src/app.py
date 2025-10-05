import zipfile
from gevent import monkey; monkey.patch_all()
from bottle import get, post, run, request
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
		with zipfile.ZipFile(cwd + "challenge.zip", "r") as zr:
			zr.extractall(cwd)
		subprocess.run("pip install -r requirements.txt", shell=True, cwd=cwd)
		initialized.add(h)

	return uid

@get("/package/:h")
def package(h):
	cachePath = f"cache/{h}/"
	if not os.path.exists(cachePath):
		os.mkdir(cachePath)
		return "false"
	return "true"

@post("/package/:h")
def package_post(h):
	cachePath = f"cache/{h}/"
	for item in request.files:
		item.save(cachePath + "challenge.zip", overwrite=True)
	return "OK"

os.mkdir("cache")
run(host="0.0.0.0", server='gevent')