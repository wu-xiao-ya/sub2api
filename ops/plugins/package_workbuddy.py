"""Build a signed .s2plugin from already compiled WorkBuddy binaries.

Requires cryptography. Never prints private key material or includes it in the ZIP.
The first release uses --create-key with a private path outside tracked source.
"""
import argparse
import base64
import hashlib
import json
from pathlib import Path
import zipfile

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', action='append', required=True, help='OS-ARCH=PATH')
    parser.add_argument('--key', type=Path, required=True)
    parser.add_argument('--create-key', action='store_true')
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--tested-version', action='append', default=[])
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]
    source = root / 'backend/plugins/workbuddy'
    if args.create_key:
        args.key.parent.mkdir(parents=True, exist_ok=True)
        key = Ed25519PrivateKey.generate()
        # Exclusive creation prevents silently replacing an existing publisher.
        with args.key.open('xb') as stream:
            stream.write(key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()))
        args.key.chmod(0o600)
    key = serialization.load_pem_private_key(args.key.read_bytes(), password=None)
    if not isinstance(key, Ed25519PrivateKey):
        raise ValueError('Ed25519 key required')
    files = {}
    runtimes = {}
    for item in args.binary:
        target, path = item.split('=', 1)
        if target not in ('linux-amd64', 'linux-arm64', 'windows-amd64'):
            raise ValueError('unsupported target')
        entry = f'runtimes/{target}/plugin' + ('.exe' if target.startswith('windows-') else '')
        files[entry] = Path(path).read_bytes()
        runtimes[target] = {'path': entry}
    for path in sorted((source / 'ui').iterdir()):
        if path.is_file():
            files['ui/' + path.name] = path.read_bytes()
    files['LICENSE.sub2api'] = (root / 'LICENSE').read_bytes()
    files['LICENSE.reference'] = (source / 'LICENSE.reference').read_bytes()
    files['THIRD_PARTY_NOTICES.md'] = (source / 'THIRD_PARTY_NOTICES.md').read_bytes()
    manifest = {
        'schema_version': 1, 'id': 'org.starlight.workbuddy', 'name': 'WorkBuddy 国内版 DeepSeek',
        'version': '0.1.1', 'author': 'Starlight',
        'description': 'Browser authorization and independently scheduled DeepSeek accounts; live acceptance required.',
        'requires': {'sub2api': '>=0.2.18 <0.3.0', 'recommended_sub2api_version': '0.2.18',
                     'tested_sub2api_versions': args.tested_version, 'plugin_protocol': 1, 'transport_api': 1, 'ui_bridge': 1},
        'capabilities': [{'id': 'upstream.chat_completions.v1', 'platform': 'deepseek', 'account_type': 'plugin'}],
        'authorization_hosts': ['www.codebuddy.cn'],
        'runtimes': runtimes, 'ui': {'entrypoint': 'ui/index.html'},
        'files': {path: hashlib.sha256(data).hexdigest() for path, data in files.items()},
    }
    raw = json.dumps(manifest, ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode()
    signature = {'algorithm': 'ed25519', 'key_id': 'starlight-workbuddy-v1',
                 'signature': base64.b64encode(key.sign(raw)).decode()}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(args.output, 'w', zipfile.ZIP_DEFLATED) as archive:
        members = {'manifest.json': raw, 'signature.json': json.dumps(signature).encode(), **files}
        for path, data in sorted(members.items()):
            info = zipfile.ZipInfo(path)
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = (0o100755 if path.startswith('runtimes/') else 0o100644) << 16
            archive.writestr(info, data)
    public = key.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
    # Public metadata is safe to retain with release reports.
    metadata = {'package': args.output.name, 'sha256': hashlib.sha256(args.output.read_bytes()).hexdigest(),
                'publisher_key_id': signature['key_id'], 'publisher_public_key': base64.b64encode(public).decode(),
                'tested_sub2api_versions': args.tested_version}
    args.output.with_suffix('.release.json').write_text(json.dumps(metadata, indent=2), encoding='utf-8')
    print(json.dumps(metadata))


if __name__ == '__main__':
    main()
