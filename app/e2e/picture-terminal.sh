# Sourced by android-picture.sh and ios-picture.sh once L (hc's isolated environment), hc, work and app are set:
# picture_terminal starts the terminal they tap. Its program shows three paths: a picture's, a text file's named .png
# and one only mentioned (no such file), then a shell whose prompt is "acme-app % " (the computer's name never shows).
#   Here (default): the project is $work/home/src/acme-app.
#   HM_PICTURE_IMAGE=<docker image>: the terminal is on machine "box", a container of that image (no network, no host
#   folders) that hc reaches with `hc machine add --via "docker exec"`, the project at /Users/sam/src/acme-app: no
#   path of this computer shows, for screenshots to publish. Needs docker; DOCKER_HOST says which daemon (L passes it).
# Sets term (the terminal's address), space (its space's id on the home screen) and full (the picture's full path as
# the sheet should say it). picture_cleanup removes the container.
box=""
show='clear
printf "%s\n" "⏺ Read(art/sunset.png)" "  ⎿  Read image (4.9KB)" "" "⏺ Read(notes.png)" "  ⎿  Read 1 line" "" "⏺ Next: art/sunset-wide.png" ""
PS1="acme-app % " exec sh -i'

picture_terminal() {
  python3 -c 'import base64,re,sys; d=re.search(r"base64,([A-Za-z0-9+/=]+)", open(sys.argv[1]).read()).group(1); open(sys.argv[2],"wb").write(base64.b64decode(d))' \
    "$app/fixtures/picture.ts" "$work/picture.png"
  if [ -n "${HM_PICTURE_IMAGE:-}" ]; then
    docker=$(command -v docker)
    box=hmpicture-$$
    "$docker" run -d --name "$box" --network none --entrypoint sleep "$HM_PICTURE_IMAGE" infinity >/dev/null
    "$docker" exec "$box" sh -c 'mkdir -p /Users/sam/src/acme-app/art && printf "just text\n" > /Users/sam/src/acme-app/notes.png'
    "$docker" cp "$work/picture.png" "$box:/Users/sam/src/acme-app/art/sunset.png"
    printf '%s\n' "$show" | "$docker" exec -i "$box" sh -c 'cat > /Users/sam/show.sh'
    L "$hc" machine add --via "$docker exec -i $box sh -c" --name box --install --json >/dev/null
    L "$hc" run --on box --bg --keep --cwd /Users/sam/src/acme-app --label herdr.space=sppicture --label herdr.tab=tbpicture \
      -- sh /Users/sam/show.sh >/dev/null
    term=$(L "$hc" ls --json | python3 -c 'import json,sys; print([t["address"] for t in json.load(sys.stdin)["terminals"] if t["address"].startswith("box/")][-1])')
    space=box/sppicture
    full=/Users/sam/src/acme-app/art/sunset.png
  else
    mkdir -p "$work/home/src/acme-app/art"
    cp "$work/picture.png" "$work/home/src/acme-app/art/sunset.png"
    printf 'just text\n' > "$work/home/src/acme-app/notes.png"
    printf '%s\n' "$show" > "$work/show.sh"
    L "$hc" run --bg --keep --cwd "$work/home/src/acme-app" --label herdr.space=sppicture --label herdr.tab=tbpicture \
      -- /bin/sh "$work/show.sh" >/dev/null
    term=t1 space=sppicture
    # As herdr-core says the folder: symlinks resolved (/private/var/… for a temp folder on macOS).
    full=$(L "$hc" get t1 --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["cwd"])')/art/sunset.png
  fi
}

picture_cleanup() { [ -z "$box" ] || "$docker" rm -f "$box" >/dev/null 2>&1 || true; }
