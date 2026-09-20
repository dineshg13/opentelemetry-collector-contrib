#!/usr/bin/env python3
# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Verify both HTTP product gates in kind while standard OTLP remains functional."""
import copy
from datetime import datetime, timezone
import json
from pathlib import Path
import subprocess
import time
import yaml

ROOT = Path(__file__).resolve().parents[1]
KUBE = ['kubectl', '--context', 'kind-otel-dd', '-n', 'ddot-poc']


def main():
    assert subprocess.check_output(['kubectl', 'config', 'current-context'], text=True).strip() == 'kind-otel-dd'
    base = list(yaml.safe_load_all((ROOT / 'kind/collectors.yaml').read_text()))
    created = []
    result = {'timestamp':datetime.now(timezone.utc).isoformat(),'scope':'synthetic protocol/control checks; not backend product readback','variants':{}}
    try:
        for name in ['collector','collector-http-forwarder']:
            off = name + '-disabled-check'
            config = yaml.safe_load((ROOT / 'collector' / ('combined.yaml' if name == 'collector' else 'combined-http-forwarder.yaml')).read_text())
            config['receivers'].pop('postgresql/dbm', None)
            for signal in ['metrics/dbm','logs/dbm']:
                config['service']['pipelines'].pop(signal, None)
            config['exporters']['debug']['verbosity'] = 'detailed'
            config['processors']['transform/disable_llm'] = {'trace_statements':[{'context':'span','statements':['set(attributes["dd_llmobs_enabled"], false)']}]}
            config['service']['pipelines']['traces']['processors'].insert(0,'transform/disable_llm')
            if name == 'collector':
                for value in config['extensions']['datadog']['products'].values():
                    value['enabled'] = False
            else:
                for route in config['extensions']['http_forwarder']['routes']:
                    if route['path'] == '/info':
                        info = json.loads(route['response']['body']);info['endpoints']=[];route['response']['body']=json.dumps(info)
                    else:
                        route['disabled']=True
            for original in base:
                if original['metadata']['name'] != name:
                    continue
                obj = copy.deepcopy(original)
                obj['metadata']['name']=off
                if obj['kind']=='ConfigMap':
                    obj['data']['collector.yaml']=yaml.safe_dump(config,sort_keys=False)
                elif obj['kind']=='Service':
                    obj['spec']['selector']['app']=off
                else:
                    obj['spec']['selector']['matchLabels']['app']=off
                    obj['spec']['template']['metadata']['labels']['app']=off
                    obj['spec']['template']['spec']['volumes'][0]['configMap']['name']=off
                subprocess.run(KUBE+['apply','-f','-'],input=json.dumps(obj),text=True,check=True)
                created.append(obj['kind'].lower()+'/'+off)
            subprocess.run(KUBE+['rollout','status','deployment/'+off,'--timeout=180s'],check=True)
            routes = yaml.safe_load((ROOT/'collector/combined-http-forwarder.yaml').read_text())['extensions']['http_forwarder']['routes']
            routes = [{'path':x['path'],'headers':x.get('match_headers',{})} for x in routes if x['path']!='/info']
            program = '''import urllib.request,urllib.error,json,time
base="http://"+NAME+".ddot-poc.svc.cluster.local"
info=json.load(urllib.request.urlopen(base+":8126/info"));assert info["endpoints"]==[]
counts={}
for route in ROUTES:
 req=urllib.request.Request(base+":8126"+route["path"],data=b"{}",headers={"Content-Type":"application/json",**route["headers"]})
 try: urllib.request.urlopen(req);raise AssertionError("disabled route accepted")
 except urllib.error.HTTPError as e: assert e.code==404;counts[route["path"]]=e.code
now=time.time_ns()
span={"traceId":"11111111111111111111111111111111","spanId":"2222222222222222","name":"ddot-disabled-control","kind":1,"startTimeUnixNano":str(now),"endTimeUnixNano":str(now+1000000),"attributes":[{"key":"gen_ai.operation.name","value":{"stringValue":"chat"}}]}
payload={"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"ddot-disabled-control"}}]},"scopeSpans":[{"scope":{"name":"ddot-protocol-check"},"spans":[span]}]}]}
req=urllib.request.Request(base+":4318/v1/traces",data=json.dumps(payload).encode(),headers={"Content-Type":"application/json"})
with urllib.request.urlopen(req) as response: status=response.status
assert status==200
print(json.dumps({"disabled_routes":counts,"info_endpoints":info["endpoints"],"otlp_trace_status":status}))
'''
            program='NAME='+repr(off)+'\nROUTES='+repr(routes)+'\n'+program
            output=subprocess.check_output(KUBE+['exec','deploy/dbm-python','--','python','-c',program],text=True)
            value=json.loads(output)
            time.sleep(3)
            logs=subprocess.check_output(KUBE+['logs','deploy/'+off],text=True)
            value['llm_disable_attribute_preserved']='dd_llmobs_enabled: Bool(false)' in logs
            value['forwarded_product_requests']=logs.count('Forwarded HTTP product request')
            assert value['llm_disable_attribute_preserved'] and value['forwarded_product_requests']==0
            result['variants'][name]=value
        (ROOT/'evidence/kind-disabled.json').write_text(json.dumps(result,indent=2)+'\n')
        print(json.dumps(result,indent=2))
    finally:
        if created:
            subprocess.run(KUBE+['delete','--ignore-not-found=true']+created,check=True)


if __name__ == '__main__':
    main()
