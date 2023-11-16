#!/usr/bin/env python3

"""
Call this from the ./scripts/protoc_swagger_gen.sh script.
Merged protoc definitions together into 1 JSON file without duplicate keys.
"""

import os
import re
import json
import yaml
import random
import string
import argparse
from pathlib import Path
from collections import OrderedDict
from yaml.representer import SafeRepresenter

# Custom representer for OrderedDict
def dict_representer(dumper, data):
    return dumper.represent_dict(data.items())

# Add custom representer to PyYAML for OrderedDict
yaml.add_representer(OrderedDict, dict_representer)


def get_version():
    """
    Get the go.mod file Version.
    """
    current_dir = os.path.dirname(os.path.realpath(__file__))
    project_root = os.path.dirname(current_dir)

    with open(os.path.join(project_root, "go.mod"), "r") as f:
        for line in f.readlines():
            if line.startswith("module"):
                version = line.split("/")[-1].strip()
                break

    if not version:
        print("Could not find version in go.mod")
        exit(1)

    return version


def merge_files(directory, title, version):
    """
    Combine all individual files calls into 1 massive file.
    """
    # What we will save when all combined

    paths = {}
    definitions = {}

    json_files = [str(file) for file in Path(directory).rglob('*.json')]
    for file in json_files:
        print(f"[+] {file}")
        with open(file) as f:
            data = json.load(f)

        for key in data["paths"]:
            if key in paths.keys(): continue
            paths[key] = data["paths"][key]

        for key in data["definitions"]:
            definitions[key] = data["definitions"][key]

    return OrderedDict([
        ("swagger", "2.0"),
        ("info", {"title": title, "version": version}),
        ("consumes", ["application/json"]),
        ("produces", ["application/json"]),
        ("paths", paths),
        ("definitions", definitions),
    ])


def alter_keys(output):
    """
    Loop through all paths, then alter any keys which are "operationId" to be a random string of 20 characters.
    This is done to avoid duplicate keys in the final output (which opens 2 tabs in swagger-ui).
    """
    for path in output["paths"]:
        for method in output["paths"][path]:
            if "operationId" in output["paths"][path][method]:
                output["paths"][path][method]["operationId"] = f'{output["paths"][path][method]["operationId"]}_{" ".join(random.choices(string.ascii_uppercase + string.digits, k=5))}'

    return output


if __name__ == '__main__':

    parser = argparse.ArgumentParser(description='Merges Protoc Swagger files.')
    parser.add_argument('-d', '--directory', help='Directory containing swagger files')
    parser.add_argument('-t', '--title', help='Title for the swagger file')
    parser.add_argument('-v', '--version', help='Version for the swagger file')
    parser.add_argument('-o', '--output', help='Output file')
    args = parser.parse_args()

    version = args.version if args.version else get_version()

    output = merge_files(args.directory, args.title, version)

    output = alter_keys(output)

    with open(args.output, "w") as o:
          yaml.dump(output, o, default_flow_style=False)