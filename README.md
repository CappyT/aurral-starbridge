# aurral-starbridge

aurral-starbridge keeps the [Aurral](https://github.com/lklynet/aurral) flow tracks that you star in [Navidrome](https://www.navidrome.org).

Aurral downloads flow tracks into `_flows/` in its Downloads Folder. A flow rotation deletes them, unless the track is in the Aurral Library. Aurral does not read Navidrome stars. aurral-starbridge reads your Navidrome stars and adds each starred flow track to the Aurral Library, before the next rotation.

## How it works

At start and then at each interval, aurral-starbridge:

1. Logs in to the Navidrome native API (`POST /auth/login`) and lists your starred songs whose path starts with `_flows/` (`GET /api/song?starred=true&missing=false&path=_flows/`).
2. Keeps only the songs of the Navidrome library with the root `NAVIDROME_LIBRARY_ROOT`. It maps each song to the Aurral path: `AURRAL_ROOT` plus the song path relative to the library root.
3. Lists the done Aurral jobs (`GET /api/playlists/jobs?status=done`) and the file of each job (`GET /api/playlists/jobs/{id}/files`), and finds the job with that path.
4. Sends the job track to `POST /api/library/downloads/track`. This is the "Add to library" action of the Aurral flow page. Aurral moves the flow file into the Library.

A track that is already in the Library counts as kept. Errors on one track do not stop the other tracks. A track that fails is tried again in the next cycle. The list of kept songs is in memory only. Unstarring a song does nothing. aurral-starbridge does not delete or move files itself.

It logs JSON to stdout: one line for each kept track and one summary line for each cycle.

## Configuration

| Variable | Default | Description |
|---|---|---|
| `NAVIDROME_URL` | required | Navidrome base URL, for example `http://navidrome:4533`. |
| `NAVIDROME_USER` | required | Navidrome user whose stars count. |
| `NAVIDROME_PASSWORD` | required | Password of that user. |
| `NAVIDROME_LIBRARY_ROOT` | `/aurral` | Root of the Navidrome library that holds the Aurral Downloads Folder. |
| `AURRAL_URL` | `http://127.0.0.1:3001` | Aurral base URL. |
| `AURRAL_USER` | `NAVIDROME_USER` | Aurral user, sent in `X-Forwarded-User`. |
| `AURRAL_GROUPS` | `administrators` | Groups sent in `X-Forwarded-Groups`. |
| `AURRAL_ROOT` | `/data/aurral` | Aurral Downloads Folder, as Aurral sees it. |
| `INTERVAL` | `15m` | Time between two cycles, as a Go duration. |

The program stops at start if a required variable is missing.

## Deployment

Run aurral-starbridge as a sidecar container in the Aurral pod. Aurral then trusts its auth proxy headers, because the requests come from `127.0.0.1`. Aurral needs:

```
AUTH_PROXY_ENABLED=true
AUTH_PROXY_TRUSTED_IPS=127.0.0.1
AUTH_PROXY_ROLE_HEADER=X-Forwarded-Groups
AUTH_PROXY_ADMIN_GROUPS=administrators
```

The job files endpoint needs the admin role. Aurral sets the role of a proxy user again on each request, from the groups header. Thus `AURRAL_GROUPS` must contain an admin group of `AUTH_PROXY_ADMIN_GROUPS`. If it does not, each request changes the user to the `user` role.

The sidecar needs no volume and no port. It runs as non-root with a read-only root filesystem.

```yaml
- name: starbridge
  image: ghcr.io/cappyt/aurral-starbridge:0.1.0
  env:
    - name: NAVIDROME_URL
      value: http://navidrome:4533
    - name: NAVIDROME_USER
      valueFrom:
        secretKeyRef: {name: aurral-starbridge, key: navidrome-user}
    - name: NAVIDROME_PASSWORD
      valueFrom:
        secretKeyRef: {name: aurral-starbridge, key: navidrome-password}
  securityContext:
    runAsNonRoot: true
    readOnlyRootFilesystem: true
    allowPrivilegeEscalation: false
    capabilities:
      drop: [ALL]
```

## Build

```sh
go test ./...
docker build -t aurral-starbridge .
```

A pushed `v*` tag builds the image `ghcr.io/cappyt/aurral-starbridge` for `linux/amd64` and `linux/arm64`.

## License

Apache License 2.0. See [LICENSE](LICENSE).
