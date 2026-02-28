#!/bin/bash
DIR_NAME=$(basename "$PWD")
ARCHIVE_NAME="${DIR_NAME}.zip"
SCRIPT_NAME=$(basename "$0")

zip -r "$ARCHIVE_NAME" . -x "$ARCHIVE_NAME" -x "$SCRIPT_NAME" -x "*.git*" -x "out/*" -x "cache/*"
