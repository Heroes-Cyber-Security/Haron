#!/bin/bash

rm -f *.zip
rm -rf out/
rm -rf cache/
rm -rf __pycache__/
rm -rf .venv/
find . -type f -name "*.pyc" -delete
