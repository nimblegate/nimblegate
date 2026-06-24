#!/bin/sh
# Positive: ENV defaulted, gcloud without --project.
ENV="${1:-prod}"
gcloud builds submit --tag gcr.io/myimg
