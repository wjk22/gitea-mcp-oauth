#!/bin/sh
# Copyright 2026 The Gitea Authors. All rights reserved.
# SPDX-License-Identifier: MIT
#
# upload-r2.sh uploads a single local file to a single object key in a
# Cloudflare R2 bucket, using curl's built-in AWS SigV4 signer (R2 is
# S3-API compatible).
#
# Usage:
#   upload-r2.sh <local-file> <remote-key>
#   upload-r2.sh --check-config
#
# The second form only validates that the required environment
# variables below are set (it does not touch the network), and is
# meant to be run as an early preflight step in CI so that a missing
# R2_* secret is reported before anything is built or published.
#
# Required environment variables:
#   R2_ENDPOINT          Base URL of the R2 endpoint, e.g.
#                        https://<account>.r2.cloudflarestorage.com
#   R2_BUCKET            Destination bucket name.
#   R2_ACCESS_KEY_ID     R2 access key id.
#   R2_SECRET_ACCESS_KEY R2 secret access key.

set -eu

# check_env validates that all required R2_* environment variables are
# set and non-empty, so the validation logic only exists in one place
# for both the normal upload mode and --check-config.
check_env() {
  missing=""

  if [ -z "${R2_ENDPOINT:-}" ]; then
    missing="$missing R2_ENDPOINT"
  fi
  if [ -z "${R2_BUCKET:-}" ]; then
    missing="$missing R2_BUCKET"
  fi
  if [ -z "${R2_ACCESS_KEY_ID:-}" ]; then
    missing="$missing R2_ACCESS_KEY_ID"
  fi
  if [ -z "${R2_SECRET_ACCESS_KEY:-}" ]; then
    missing="$missing R2_SECRET_ACCESS_KEY"
  fi

  if [ -n "$missing" ]; then
    echo "upload-r2.sh: missing required environment variable(s):$missing" >&2
    exit 1
  fi
}

if [ "$#" -eq 1 ] && [ "$1" = "--check-config" ]; then
  check_env
  echo "upload-r2.sh: R2 configuration OK"
  exit 0
fi

if [ "$#" -ne 2 ]; then
  echo "usage: upload-r2.sh <local-file> <remote-key>" >&2
  echo "       upload-r2.sh --check-config" >&2
  exit 1
fi

local_file="$1"
remote_key="$2"

if [ ! -f "$local_file" ]; then
  echo "upload-r2.sh: local file not found: $local_file" >&2
  exit 1
fi

check_env

# Strip a single trailing slash from the endpoint, if present, so that
# building the path-style URL below never produces a double slash.
endpoint="${R2_ENDPOINT%/}"
url="$endpoint/$R2_BUCKET/$remote_key"

# Credentials are passed to curl through a config file read from
# stdin rather than as a command-line argument, so they never show up
# in `ps` output.
#
# --fail-with-body (instead of plain --fail) still exits non-zero on
# HTTP errors, but also prints R2's XML error body, which is where the
# actual error code lives (SignatureDoesNotMatch, NoSuchBucket,
# AccessDenied, ...). --retry 3 (without --retry-all-errors) only
# retries the transient cases (5xx, 408, 429, connection failures).
printf 'user = "%s:%s"\n' "$R2_ACCESS_KEY_ID" "$R2_SECRET_ACCESS_KEY" | curl \
  --config - \
  --fail-with-body \
  --silent \
  --show-error \
  --retry 3 \
  --aws-sigv4 "aws:amz:auto:s3" \
  --upload-file "$local_file" \
  "$url"
