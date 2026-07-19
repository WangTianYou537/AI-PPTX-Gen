# Deploy & Upgrade

## 1) Local / CI build

```bash
# full single-binary build
./build.sh

# multi-arch release packages
./scripts/package-release.sh --version 1.0.0
# outputs in dist/
```

GitHub Actions:

- `CI` on push/PR: lint/typecheck/test/build binary artifact
- `Release` on tag `v*`: publish multi-arch `.tar.gz` + scripts

## 2) First deploy on server

```bash
# from extracted release package
sudo ./deploy.sh --port 18080 --dir /opt/ppt-gen

# or from repo after ./build.sh
sudo ./scripts/deploy.sh --bin ./bin/ppt-gen --port 18080
```

Then open `http://SERVER:18080`.

## 3) Upgrade

```bash
# binary file
sudo ./upgrade.sh --artifact ./ppt-gen

# or release archive
sudo ./upgrade.sh --artifact ./ppt-gen_1.0.0_linux_amd64.tar.gz

# with data snapshot
sudo ./upgrade.sh --artifact ./ppt-gen --backup-data

# manual rollback
sudo ./upgrade.sh --rollback
```

## Notes

- Runtime data is under `APP_DIR/data` (default `/opt/ppt-gen/data`)
- Config: `/opt/ppt-gen/config.yml`
- Env: `/opt/ppt-gen/env`
- PPTX export requires `officecli` on the host
- Debug export failures keep files under `data/export-debug/` when `DEBUG=1`
