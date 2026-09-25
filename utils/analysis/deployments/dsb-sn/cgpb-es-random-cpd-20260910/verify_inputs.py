#!/usr/bin/env python3
"""Read stored Lua posts and verify matched request inputs across CPD phases."""
import hashlib
import json
import re

import run_phase as run

pod = next(p['metadata']['name'] for p in run.pods()['items'] if p['metadata']['name'].startswith('post-db-'))
all_hashes = {}
for phase in run.SPEC['phases']:
    directory = run.ROOT / phase['name']
    posts = json.loads((directory / 'new-posts.json').read_text())
    ids = [re.fullmatch(r'ObjectId\("([0-9a-f]{24})"\)', p['id']).group(1) for p in posts]
    js_ids = ','.join('ObjectId(' + json.dumps(oid) + ')' for oid in ids)
    js = 'var p=db.getSiblingDB("post").getCollection("post"); print(JSON.stringify(p.find({_id:{$in:[' + js_ids + ']}}).sort({_id:1}).toArray()));'
    documents = json.loads(run.kube('exec', pod, '--', 'mongo', '--quiet', '--eval', js))
    assert len(documents) == len(posts)
    run.save(directory / 'stored-posts.json', documents)
    hashes = []
    for post in documents:
        text = post['text']
        for url in sorted(post['urls'], key=lambda u: len(u['shortenedurl']), reverse=True):
            text = text.replace(url['shortenedurl'], url['expandedurl'])
        normalized = {'creator': post['creator'], 'text': text,
            'usermentions': sorted(post['usermentions'], key=lambda m: json.dumps(m, sort_keys=True)),
            'medias': sorted(post['medias'], key=lambda m: json.dumps(m, sort_keys=True)),
            'urls': sorted(u['expandedurl'] for u in post['urls']), 'posttype': post['posttype']}
        hashes.append(hashlib.sha256(json.dumps(normalized, sort_keys=True).encode()).hexdigest())
    all_hashes[phase['name']] = hashes
    run.save(directory / 'input-hashes.json', hashes)
assert all_hashes['fixed2'] == all_hashes['fixed4'] == all_hashes['random2_6'], 'Stored request inputs differ'
run.save(run.ROOT / 'input-equivalence.json', {'matched': True,
    'requests_per_phase': len(all_hashes['fixed2']), 'lua_seed': 42,
    'normalization': 'Exclude generated post/req/Object IDs; expand shortened URLs; compare creator, original text, mentions, media IDs/types, expanded URLs and post type.',
    'hashes': all_hashes})
print('All stored request inputs match across the three phases, in request order.')
