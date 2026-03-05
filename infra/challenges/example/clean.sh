#!/bin/bash

rm -f *.zip
rm -rf out/
rm -rf cache/
rm -rf __pycache__/
rm -rf .venv/
rm -rf lib/
rm -rf src/
rm -rf test/
rm -rf script/
rm -f foundry.toml
find . -type f -name "*.pyc" -delete
