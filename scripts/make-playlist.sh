#!/bin/bash
# Self-contained macOS launcher. Python source is embedded below. No dependencies from pip.
set -eu
if ! command -v python3 >/dev/null 2>&1; then
  printf '%s\n' 'Python 3.8+ is required. See README-standalone.md before proceeding.'
  exit 1
fi
python3 -c 'import sys; sys.exit(0 if sys.version_info >= (3,8) else 1)' || {
  printf '%s\n' 'Python 3.8 or later is required.'; exit 1;
}
exec python3 - "$@" <<'PERMANENT_ROTATION_PYTHON'
import argparse,base64,hashlib,http.server,json,os,re,secrets,ssl,subprocess,sys,time,urllib.error,urllib.parse,urllib.request
from pathlib import Path
from html.parser import HTMLParser

SITE='https://todkap.github.io/permanent-rotation/index.html'
ARTICLE='https://raw.githubusercontent.com/todkap/permanent-rotation/main/article/README.md'
REDIRECT='http://127.0.0.1:8888/callback'
API='https://api.spotify.com/v1'
STATE=Path.home()/'Library/Application Support/Permanent Rotation/playlist-state.json'
DEBUG=False
STEP='startup'
class Stop(Exception):pass
def progress(step):
    global STEP
    STEP=step
    if DEBUG:print('[debug] '+step,flush=True)

def ask(label,default=None):
    progress('Read interactive prompt: '+label)
    try:
        with open('/dev/tty','w') as output, open('/dev/tty','r') as input_tty:
            output.write(label+(' ['+default+']' if default is not None else '')+': ');output.flush()
            value=input_tty.readline()
    except (OSError,ValueError) as e:
        raise Stop('Cannot read your Terminal prompt ['+type(e).__name__+']. Run the script directly in Terminal, not an editor or redirected console. Nothing further submitted.')
    if value=='':raise Stop('Input closed. Nothing further submitted.')
    return value.strip() or (default if default is not None else '')

def fetch(url):
    host=urllib.parse.urlparse(url).hostname
    progress('Fetch public source from '+str(host))
    if urllib.parse.urlparse(url).scheme!='https':raise Stop('Source must use HTTPS.')
    try:
        with urllib.request.urlopen(url,timeout=30) as r:
            data=r.read()
            if DEBUG:print('[debug] HTTP %s; %d bytes'%(r.status,len(data)))
            return data.decode('utf-8')
    except urllib.error.HTTPError as e:raise Stop('Public source '+str(host)+' returned HTTP '+str(e.code)+'. Retry later or check network access.')
    except urllib.error.URLError as e:
        reason=e.reason
        if isinstance(reason,ssl.SSLCertVerificationError) or 'CERTIFICATE_VERIFY_FAILED' in str(reason):
            raise Stop('HTTPS certificate verification failed for '+str(host)+'. Python.org install: run Applications > Python 3.x > Install Certificates.command, then retry. Homebrew Python: no separate Install Certificates.command step is normally needed. Check python3 location with: python3 -c \"import sys; print(sys.executable)\". If Homebrew also fails, ask for help with its CA/network setup. Do NOT disable HTTPS verification. [URLError/'+type(reason).__name__+']')
        raise Stop('Could not reach public source '+str(host)+' [URLError/'+type(reason).__name__+']. Check Wi-Fi, VPN/proxy or retry later. No Spotify changes.')
    except Exception as e:raise Stop('Public-source fetch failed on '+str(host)+' ['+type(e).__name__+']. No Spotify changes.')

class Cards(HTMLParser):
    def __init__(self):
        super().__init__();self.depth=0;self.card=None;self.h3=False;self.tier=False;self.byline=False;self.cards=[]
    def handle_starttag(self,tag,attrs):
        a=dict(attrs)
        if tag=='div':
            if self.depth:self.depth+=1
            elif 'body' in a.get('class','').split():self.depth=1;self.card={'title':'','artist_line':'','uri':None}
        if self.depth:
            if tag=='p' and 'byline' in a.get('class','').split():self.byline=True
            if tag=='h3':self.h3=True
            if tag=='span' and self.h3:self.tier=True
            if tag=='a':
                m=re.fullmatch(r'https://open\.spotify\.com/track/([A-Za-z0-9]{22})',a.get('href',''))
                if m:self.card['uri']='spotify:track:'+m[1]
    def handle_endtag(self,tag):
        if tag=='p':self.byline=False
        if tag=='h3':self.h3=False
        if tag=='span':self.tier=False
        if tag=='div' and self.depth:
            self.depth-=1
            if self.depth==0:self.card['title']=self.card['title'].strip();self.cards.append(self.card);self.card=None
    def handle_data(self,s):
        if self.depth and self.h3 and not self.tier:self.card['title']+=s
        if self.depth and self.byline:self.card['artist_line']+=s

def manifest(html,article,site=SITE,name=None):
    progress('Parse timeline cards and article title')
    p=Cards();p.feed(html)
    if not p.cards:raise Stop('No song cards found. This script expects a Permanent Rotation generated timeline.')
    missing=[c['title'] for c in p.cards if not c['uri']]
    if len(p.cards)>100:raise Stop('This one-shot script supports up to 100 tracks; no changes made.')
    tracks=[c for c in p.cards if c['uri']]
    if not tracks:raise Stop('No Spotify tracks to add.')
    if len({c['uri'] for c in tracks})!=len(tracks):raise Stop('Duplicate Spotify IDs in the site; review before creating.')
    first=article.splitlines()[0].strip()
    if not first.startswith('# '):raise Stop('Article title not found.')
    title=name or first[2:].strip()
    if len(title)>100:raise Stop('Article title is too long for this script. Review before creating.')
    return {'name':title,'public':True,'tracks':tracks,'omitted':missing,'site':site,'description':'Songs that stayed with me, in life-chapter order. '+site}

def preview(m):
    print('\nPLAYLIST PREVIEW\nName: '+m['name']+'\nVisibility: '+('PUBLIC' if m['public'] else 'PRIVATE'))
    print('Tracks: %d, in the live page display order. Omitted (no Spotify match): %s.'%(len(m['tracks']),', '.join(m['omitted']) or 'none'))
    print('Source: '+m['site'])
    print('Description: '+m['description'])
    for i,c in enumerate(m['tracks'],1):
        print('%2d. %s | %s | %s'%(i,c['title'],c.get('artist_line','').strip(),c['uri']))
    print('No replacements for unavailable songs. Playback can still vary by account/region.')

def save(s,path=STATE):
    path.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
    tmp=path.with_suffix('.tmp');fd=os.open(str(tmp),os.O_WRONLY|os.O_CREAT|os.O_TRUNC,0o600)
    os.fchmod(fd,0o600)
    with os.fdopen(fd,'w') as f:json.dump(s,f,indent=2);f.flush();os.fsync(f.fileno())
    os.replace(str(tmp),str(path))

def oauth(client,public):
    verifier=secrets.token_urlsafe(64);state=secrets.token_urlsafe(32)
    challenge=base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).rstrip(b'=').decode()
    result={}
    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self,*a):pass
        def do_GET(self):
            u=urllib.parse.urlparse(self.path);q=urllib.parse.parse_qs(u.query)
            if u.path!='/callback':self.send_error(404);return
            if not secrets.compare_digest(q.get('state',[''])[0],state):self.send_error(400,'State mismatch');return
            if q.get('error'):result['error']='Spotify authorization was declined.'
            elif q.get('code'):result['code']=q['code'][0]
            else:self.send_error(400);return
            body=b'<html><head><meta name="referrer" content="no-referrer"></head><body>Spotify authorization received. Return to Terminal. You can close this tab.</body></html>'
            self.send_response(200);self.send_header('Content-Type','text/html');self.send_header('Cache-Control','no-store');self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
    try:server=http.server.HTTPServer(('127.0.0.1',8888),Handler)
    except OSError:raise Stop('Port 8888 is busy. Close another copy/local server and run again. Nothing created.')
    server.timeout=1
    scope=('playlist-modify-public' if public else 'playlist-modify-private')+' playlist-read-private'
    auth='https://accounts.spotify.com/authorize?'+urllib.parse.urlencode({'client_id':client,'response_type':'code','redirect_uri':REDIRECT,'scope':scope,'code_challenge_method':'S256','code_challenge':challenge,'state':state})
    print('\nOpening Spotify in your browser. Sign into your intended account and approve. Return here afterward.')
    opened=subprocess.run(['open',auth],check=False)
    if opened.returncode:
        server.server_close()
        raise Stop('Could not open the browser. Run this script on macOS with a default browser configured.')
    deadline=time.time()+300
    try:
        while not result and time.time()<deadline:server.handle_request()
    finally:server.server_close()
    if result.get('error'):raise Stop(result['error'])
    if not result.get('code'):raise Stop('Authorization timed out after five minutes. Run again; no playlist was created in this attempt.')
    data=urllib.parse.urlencode({'grant_type':'authorization_code','client_id':client,'code':result.pop('code'),'redirect_uri':REDIRECT,'code_verifier':verifier}).encode()
    req=urllib.request.Request('https://accounts.spotify.com/api/token',data=data,headers={'Content-Type':'application/x-www-form-urlencoded'})
    try:
        with urllib.request.urlopen(req,timeout=30) as r:tok=json.load(r)
    except Exception:raise Stop('Spotify token exchange failed. Check callback/app settings and retry. No token details were printed.')
    # Deliberately discard refresh_token and all response metadata.
    token=tok.get('access_token');tok.clear()
    if not token:raise Stop('Spotify returned no access token.')
    return token

class Spotify:
    def __init__(self,token):self.token=token
    def call(self,method,path,body=None):
        if not path.startswith('/'):raise Stop('Unexpected Spotify path.')
        req=urllib.request.Request(API+path,data=None if body is None else json.dumps(body).encode(),method=method,headers={'Authorization':'Bearer '+self.token,'Content-Type':'application/json'})
        try:
            with urllib.request.urlopen(req,timeout=35) as r:return json.load(r)
        except urllib.error.HTTPError as e:
            hint={401:'Authorization expired. Rerun to sign in again.',403:'Check Premium, the app user allowlist and granted playlist permissions.',429:'Spotify rate limit. Stop and retry later; state is preserved.'}.get(e.code,'See the saved state; no write was automatically retried.')
            raise Stop('Spotify HTTP %d. %s'%(e.code,hint))
        except (urllib.error.URLError,TimeoutError,ValueError,OSError):raise Stop('Spotify request could not be confirmed. State preserved. Rerun safely; no automatic create/append retry.')
    def items(self,pid):
        out=[];offset=0
        while True:
            d=self.call('GET','/playlists/'+pid+'/items?limit=100&offset='+str(offset))
            rows=d.get('items',[])
            for row in rows:
                t=row.get('item') or row.get('track') or {}
                original=t.get('linked_from') or {}
                out.append(original.get('uri') or t.get('uri'))
            if not d.get('next'):break
            if not rows:raise Stop('Unexpected empty paginated Spotify response.')
            offset+=len(rows)
        return out

def run_creation(api,s,persist):
    owner=api.call('GET','/me');uid=owner['id']
    if s.get('owner') and s['owner']!=uid:raise Stop('Signed into a different account from saved state. Nothing changed.')
    print('Recovery label appended to the playlist description: '+s['marker'])
    print('Spotify account: '+str(owner.get('display_name') or uid)+' ('+uid+')')
    if ask('Type confirm to create/resume exactly this '+('PUBLIC' if s['manifest']['public'] else 'PRIVATE')+' playlist')!='confirm':raise Stop('Canceled. Nothing further submitted.')
    s['owner']=uid
    if s.get('status')=='creating' and not s.get('playlist_id'):
        raise Stop('Earlier create response was ambiguous. This script will NOT create again. Check your Spotify for this exact recovery label: '+s['marker']+'. Keep the state file and ask for help recovering its playlist ID. Do not delete it and rerun.')
    if not s.get('playlist_id'):
        s['status']='creating';persist(s)
        # Never retry POST creation: a response lost after acceptance must not duplicate.
        p=api.call('POST','/me/playlists',{'name':s['manifest']['name'],'public':s['manifest']['public'],'description':s['manifest']['description']+' '+s['marker']})
        pid=p.get('id');link=(p.get('external_urls') or {}).get('spotify')
        if not pid or not re.fullmatch('[A-Za-z0-9]+',pid) or not link:raise Stop('Playlist creation response incomplete. State says creating; no second playlist will be made.')
        s.update(playlist_id=pid,url=link,status='created');persist(s)
    pid=s['playlist_id'];meta=api.call('GET','/playlists/'+pid)
    if (meta.get('owner') or {}).get('id')!=uid:raise Stop('Saved playlist is not owned by the signed-in account. Nothing changed.')
    expected=[c['uri'] for c in s['manifest']['tracks']]
    actual=api.items(pid)
    if actual!=expected:
        if s.get('status') not in ('created','writing'):raise Stop('Playlist was changed after completion. Refusing to overwrite your edits.')
        if actual and actual!=expected[:len(actual)]:raise Stop('Playlist contains unexpected items/order. Refusing to overwrite.')
        s['status']='writing';persist(s)
        # Idempotent exact-set PUT, NOT append: retry cannot double the tracks.
        api.call('PUT','/playlists/'+pid+'/items',{'uris':expected})
        actual=api.items(pid)
        if actual!=expected:raise Stop('Read-back did not match all approved tracks in order. State preserved; run again to inspect/resume. No new playlist will be created.')
    if meta.get('public') is not s['manifest']['public']:raise Stop('Playlist visibility differs from approved preview. Inspect it in Spotify; script will not silently change it.')
    s['status']='complete';persist(s)
    print('\nVerified %d tracks, exact approved order, owned by this account, %s.\n%s'%(len(expected),'PUBLIC' if s['manifest']['public'] else 'PRIVATE',s['url']))
    print('Keep the non-secret state file to prevent duplicate creation on future runs.')

def main():
    parser=argparse.ArgumentParser(description='Create/resume Permanent Rotation once with Spotify PKCE; no saved tokens.')
    parser.add_argument('--version',action='version',version='Permanent Rotation playlist script v4')
    parser.add_argument('--dry-run',action='store_true',help='Read live site and show preview; no OAuth, state file or Spotify writes.')
    parser.add_argument('--site',default=SITE,help='Your generated HTTPS index.html URL (forkers: set this).')
    parser.add_argument('--name',help='Exact playlist name (default: current article title).')
    parser.add_argument('--private',action='store_true',help='Create a private playlist instead of public.')
    parser.add_argument('--debug',action='store_true',help='Print non-secret step names, Python/SSL versions and public-source HTTP status; never tokens.')
    a=parser.parse_args()
    global DEBUG
    DEBUG=a.debug
    if DEBUG:
        print('[debug] Python '+sys.version.split()[0]+'; '+ssl.OPENSSL_VERSION)
        print('[debug] Version 4. Interactive prompt fix; public-source diagnostics only; no credentials printed.')
    def get_manifest():
        m=manifest(fetch(a.site),('# '+a.name) if a.name else fetch(ARTICLE),a.site,a.name)
        m['public']=not a.private
        if len(m['description'])>240:raise Stop('Source URL too long for playlist description.')
        return m
    if a.dry_run:
        preview(get_manifest());print('\nDRY RUN ONLY. No OAuth, state saved or Spotify changes.');return
    STATE.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
    lock=STATE.with_suffix('.lock')
    try:lock.mkdir(mode=0o700)
    except FileExistsError:raise Stop('Another run may be active. Close it first. If it crashed, remove ONLY the playlist-state.lock folder after confirming no script is running. Keep playlist-state.json.')
    try:
        if STATE.exists():
            s=json.loads(STATE.read_text())
            if s['manifest']['site']!=a.site:raise Stop('Saved state belongs to another site. Keep it intact; do not overwrite a prior playlist.')
            print('Using saved, previously previewed manifest. Name/visibility flags do not alter an existing run.')
        else:
            s={'version':1,'manifest':get_manifest(),'marker':'[pr-'+secrets.token_hex(6)+']','status':'new'}
        preview(s['manifest'])
        print('\nRecovery state (non-secret playlist/track IDs only): '+str(STATE))
        client=os.environ.pop('SPOTIFY_CLIENT_ID','') or ask('Spotify Client ID (NOT Client Secret)')
        if not re.fullmatch('[a-fA-F0-9]{32}',client):raise Stop('Client ID should be 32 hexadecimal characters. Do not enter a Client Secret.')
        token=oauth(client,s['manifest']['public'])
        run_creation(Spotify(token),s,save)
    finally:
        try:lock.rmdir()
        except OSError:pass

if __name__=='__main__':
    try:main()
    except KeyboardInterrupt:print('\nStopped. Keep the state file; rerun to resume. No write automatically retried.');sys.exit(1)
    except Stop as e:print('\nSTOP: '+str(e));sys.exit(1)
    except Exception as e:print('\nSTOP: '+STEP+' failed ['+type(e).__name__+']. No details or credentials printed. Keep state and ask for help.');sys.exit(1)

PERMANENT_ROTATION_PYTHON
