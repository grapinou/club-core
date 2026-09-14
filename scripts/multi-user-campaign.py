"""Run once per fresh cmd/multi-user fixture. Requires Python Playwright.
All credentials and evidence stay beside the private state file; no mail contents
or credentials are printed. Run from the repository root.
"""
import json
import os
from pathlib import Path
import subprocess
import sys
from playwright.sync_api import sync_playwright

os.umask(0o077)
state_path = Path(sys.argv[1])
s = json.loads(state_path.read_text())
out = state_path.parent / 'browser'
out.mkdir(mode=0o700, exist_ok=True)
results = []
note = '<script>window.campaignLeak=1</script> NOTE_INTERNE_CAMPAGNE'
address = 'Adresse fictive campagne A — mise à jour'

def check(ok, label):
    if not ok:
        print('Échec du contrôle :', label)
        raise RuntimeError(label)
    results.append(label)

def control(cmd):
    r = subprocess.run(['go', 'run', './cmd/multi-user', cmd, str(state_path)], capture_output=True)
    check(r.returncode == 0, 'commande ' + cmd)

def go(page, path, status=200):
    r = page.goto(s['BaseURL'] + path)
    check(r.status == status, path + ' HTTP ' + str(status))
    check(r.headers.get('cache-control') == 'no-store', path + ' no-store')
    return page.locator('body').inner_text()

def submit(page, action):
    with page.expect_navigation() as nav:
        page.locator('form[action="' + action + '"] button[type="submit"], form[action="' + action + '"] button:not([type])').first.click()
    check(nav.value.status == 200, 'formulaire ' + action)

def personal(page, alias):
    check('Bonjour User ' + alias + '.' in go(page, '/me'), 'me reste propre ' + alias)
    go(page, '/me/account')
    check(page.locator('dd').all_text_contents()[1] == alias, 'identité personnelle ' + alias)
    check(s['Accounts'][alias]['Username'] in page.locator('body').inner_text(), 'username propre ' + alias)

def shot(page, alias, path, label, width, status=200):
    page.set_viewport_size({'width':width,'height':900})
    body = go(page, path, status)
    check('NOTE_INTERNE_CAMPAGNE' not in body if alias != 'B' else True, 'notes isolées ' + label)
    check(page.evaluate('document.documentElement.scrollWidth <= innerWidth'), 'sans overflow ' + label + ' ' + str(width))
    check(page.locator('.admin-nav').count() == (1 if alias == 'B' and status != 403 else 0), 'navigation ' + label)
    check(status != 200 or bool(page.evaluate("getComputedStyle(document.documentElement).getPropertyValue('--bs-body-font-family')")), 'CSS chargé ' + label)
    page.screenshot(path=str(out / (alias + '-' + label + '-' + str(width) + '.png')), full_page=True)

try:
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        contexts = {a:browser.new_context(java_script_enabled=False if a == 'A' else True, viewport={'width':1365,'height':900}) for a in 'ABC'}
        pages = {a:c.new_page() for a,c in contexts.items()}
        for alias,page in pages.items():
            go(page, '/login')
            page.locator('#username').fill(s['Accounts'][alias]['Username'])
            page.locator('#password').fill(s['Accounts'][alias]['Password'])
            submit(page, '/login')
            personal(page, alias)
        cookies = [{c['value'] for c in ctx.cookies() if 'session' in c['name']} for ctx in contexts.values()]
        check(all(cookies) and all(not cookies[i]&cookies[j] for i in range(3) for j in range(i)), 'trois cookies de session distincts')
        A,B,C = [pages[a] for a in 'ABC']
        child = '/me/children/' + str(s['Child'])
        own = '/me/memberships/' + str(s['MembershipA'])
        admin = '/memberships/' + str(s['MembershipA'])
        person = '/persons/' + str(s['Accounts']['A']['Person'])
        for page in (A,C): go(page, '/admin',403)
        for page in (A,B,C): go(page, child,404)
        for page in (B,C): go(page, own,404)
        go(A, '/me/account/profile')
        A.locator('#address').fill(address)
        submit(A, '/me/account/profile')
        go(B, '/persons')
        B.locator('#search').fill('User A')
        submit(B, '/persons/search')
        B.locator('a[href="' + person + '"]').first.click()
        check(address in B.locator('body').inner_text(), 'A self-service visible chez B après recherche')
        B.locator('#notes').fill(note)
        submit(B, person + '/notes')
        check('&lt;script&gt;' in B.content() and B.locator('script').count() == 1, 'note Person échappée')
        go(B, admin)
        B.locator('#admin_note').fill(note)
        submit(B, admin + '/approve')
        go(B, admin + '/notes')
        B.locator('#notes').fill(note + ' modifiée')
        submit(B, admin + '/notes')
        go(B, admin + '/groups')
        B.locator('#group_id').select_option(str(s['Group']))
        B.locator('#joined_at').fill(s['Today'])
        submit(B, admin + '/groups')
        body = go(A, own)
        check('Active' in body and 'Groupe campagne' in body and 'Adhésion campagne' in body, 'A voit statut type saison et groupe traités par B')
        for path in ['/dashboard','/me','/me/account',own]:
            check('NOTE_INTERNE_CAMPAGNE' not in go(A,path), 'note absente A ' + path)
        # The existing Trial form uses A's existing Person and explicit consent.
        go(B, '/trials/' + str(s['Trial']))
        B.locator('a[href*="memberships/new"]').click()
        B.locator('#season_id').select_option(str(s['NextSeason']))
        B.locator('#type_id').select_option(str(s['Kind']))
        B.locator('#giver_id').select_option(str(s['Accounts']['A']['Person']))
        B.locator('#consent-' + str(s['Consent'])).select_option('refused')
        submit(B, person + '/memberships/new')
        new_id = B.url.split('/')[-1].split('?')[0]
        body = go(A, '/me/memberships/' + new_id)
        check('Saison suivante' in body and 'Refusé' in body and 'Groupe campagne' not in body, 'Trial vers Membership visible sans groupe automatique et consentement refusé')
        personal(B, 'B')
        go(B, '/me?person_id=' + str(s['Accounts']['A']['Person']))
        personal(B, 'B')
        go(B, '/me/account/profile')
        B.locator('#address').fill('Adresse fictive propre à B')
        B.locator('form[action="/me/account/profile"]').evaluate('(form, ids) => { for (const [name,value] of Object.entries(ids)) { const field=document.createElement("input"); field.type="hidden"; field.name=name; field.value=value; form.appendChild(field); }}', {'person_id':s['Accounts']['A']['Person'], 'user_id':s['Accounts']['A']['User']})
        submit(B, '/me/account/profile')
        check('Adresse fictive propre à B' in go(B, '/me/account'), 'IDs forgés modifient seulement B')
        check(address in go(A, '/me/account'), 'IDs forgés de B ne modifient pas A')
        control('grant')
        go(C, child)
        go(C, child + '/memberships/' + str(s['MembershipChild']))
        for page in (A,B): go(page, child,404)
        for width in (390,1365):
            for path,label in [('/dashboard','dashboard'),('/me/account','account'),(own,'membership')]: shot(A,'A',path,label,width)
            for path,label in [('/admin','admin'),('/persons','persons'),(person,'person-A'),(admin,'membership-A'),('/trials','trials')]: shot(B,'B',path,label,width)
            for path,label in [('/dashboard','dashboard'),(child,'child')]: shot(C,'C',path,label,width)
        control('revoke')
        for width in (390,1365): shot(C,'C',child,'child-revoked',width,404)
        go(C, child + '/memberships/' + str(s['MembershipChild']),404)
        personal(C,'C')
        go(C,'/admin',403)
        control('revoke-role')
        go(B,'/admin',403)
        go(B,person,403)
        go(B,child,404)
        personal(B,'B')
        check(B.locator('.admin-nav').count() == 0, 'navigation B retirée immédiatement')
        control('grant-role')
        go(B,'/admin')
        go(B,child,404)
        audit = subprocess.run(['go','run','./cmd/multi-user','audit',str(state_path)],capture_output=True,check=True)
        data=json.loads(audit.stdout)
        check(data['persons']==4 and data['users']==3 and data['child_users']==0 and data['trial_status']=='registered', 'aucune duplication Person ni User enfant ni mutation Trial')
        check(all(e['actor']==s['Accounts']['B']['User'] for e in data['events']), 'audit administratif acteur B')
        (out/'domain-audit.json').write_text(json.dumps(data,indent=2))
        check(cookies == [{c['value'] for c in ctx.cookies() if 'session' in c['name']} for ctx in contexts.values()], 'sessions initiales conservées après les révocations')
        browser.close()
    (out/'checks.json').write_text(json.dumps(results,ensure_ascii=False,indent=2))
    print('Campagne réussie :',len(results),'contrôles ; preuves privées dans',out)
except Exception as e:
    (out/'checks.json').write_text(json.dumps(results,ensure_ascii=False,indent=2))
    print('Campagne interrompue après',len(results),'contrôles ; type:',type(e).__name__)
    # Do not print Playwright exceptions: call logs may contain credential values.
    sys.exit(1)
