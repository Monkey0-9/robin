"""
Robin Trading Platform — Secret Vault
AES-GCM encrypted storage for API keys.
"""
import os
import sys
import json
import base64
import argparse
from cryptography.hazmat.primitives.ciphers.aead import AESGCM

VAULT_FILE = os.path.abspath(os.path.join(os.path.dirname(__file__), '..', '..', '.secrets', 'vault.enc'))

def get_key(env_var="ROBIN_MASTER_KEY"):
    key_b64 = os.environ.get(env_var)
    if not key_b64:
        raise ValueError(f"{env_var} environment variable not set.")
    return base64.b64decode(key_b64)

def load_vault(key):
    if not os.path.exists(VAULT_FILE):
        return {}
    with open(VAULT_FILE, 'rb') as f:
        data = f.read()
    nonce, ct = data[:12], data[12:]
    aesgcm = AESGCM(key)
    try:
        pt = aesgcm.decrypt(nonce, ct, None)
        return json.loads(pt.decode('utf-8'))
    except Exception as e:
        print("Failed to decrypt vault. Wrong master key?", file=sys.stderr)
        sys.exit(1)

def save_vault(key, data):
    aesgcm = AESGCM(key)
    nonce = os.urandom(12)
    pt = json.dumps(data).encode('utf-8')
    ct = aesgcm.encrypt(nonce, pt, None)
    os.makedirs(os.path.dirname(VAULT_FILE), exist_ok=True)
    with open(VAULT_FILE, 'wb') as f:
        f.write(nonce + ct)
    # Ensure tight permissions on Windows/Linux
    try:
        os.chmod(VAULT_FILE, 0o600)
    except:
        pass

def migrate_env(env_path):
    print(f"Migrating keys from {env_path}")
    secrets = {}
    if os.path.exists(env_path):
        with open(env_path, 'r') as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith('#'):
                    continue
                if '=' in line:
                    k, v = line.split('=', 1)
                    if any(x in k for x in ['API', 'SECRET', 'TOKEN', 'KEY']):
                        secrets[k] = v.strip(' "\'')
    if secrets:
        key = get_key()
        save_vault(key, secrets)
        print(f"Migrated {len(secrets)} secrets to vault.")
    else:
        print("No secrets found in .env to migrate.")

def rotate_keys():
    old_key = get_key("ROBIN_MASTER_KEY_OLD")
    new_key = get_key("ROBIN_MASTER_KEY")
    data = load_vault(old_key)
    save_vault(new_key, data)
    print("Vault re-encrypted with new master key.")

def get_secret(secret_key):
    key = get_key()
    data = load_vault(key)
    if secret_key in data:
        print(data[secret_key])
    else:
        sys.exit(1)

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Robin Secret Vault")
    parser.add_argument('--init', action='store_true', help='Initialize empty vault')
    parser.add_argument('--migrate-env', type=str, help='Migrate .env file to vault')
    parser.add_argument('--rotate', action='store_true', help='Rotate master key')
    parser.add_argument('--get', type=str, help='Get a secret by key')
    
    args = parser.parse_args()
    
    if args.init:
        save_vault(get_key(), {})
        print("Empty vault initialized.")
    elif args.migrate_env:
        migrate_env(args.migrate_env)
    elif args.rotate:
        rotate_keys()
    elif args.get:
        get_secret(args.get)
    else:
        parser.print_help()
