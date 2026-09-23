# Copyright The OpenTelemetry Authors
# SPDX-License-Identifier: Apache-2.0
"""Read-only official US5 Datadog MCP client. Credentials and session stay in memory."""
import json,urllib.request,urllib.error
from client import Client
ALLOWED_TOOLS=frozenset({'get_profiling_profile_types','get_profiling_services','get_profiling_runtime_ids','explore_profiling_flame_graph','get_spark_job_health','get_spark_sql_plan','search_data_entities','get_data_entity_lineage'})
class MCPClient:
 def __init__(self,toolsets='profiling'):
  if toolsets not in ('profiling','data-observability'):
   raise ValueError('Only assigned read toolsets are allowed')
  c=Client();self.opener=c.opener
  self.headers={'Accept':'application/json, text/event-stream','Content-Type':'application/json','DD_API_KEY':c.headers['DD-API-KEY'],'DD_APPLICATION_KEY':c.headers['DD-APPLICATION-KEY']}
  self.url='https://mcp.us5.datadoghq.com/v1/mcp?toolsets='+toolsets
  self.counter=0
 def rpc(self,method,params=None):
  if method not in ('initialize','notifications/initialized','tools/list','tools/call'):raise ValueError('Method blocked')
  if method=='tools/call' and params.get('name') not in ALLOWED_TOOLS:raise ValueError('Tool not in explicit read-only allowlist')
  body={'jsonrpc':'2.0','method':method}
  if method!='notifications/initialized':self.counter+=1;body['id']=self.counter
  if params is not None:body['params']=params
  req=urllib.request.Request(self.url,data=json.dumps(body).encode(),headers=self.headers,method='POST')
  try:r=self.opener.open(req,timeout=30)
  except urllib.error.HTTPError as e:r=e
  except Exception:return 0,{'error':{'code':'transport_error','message':'details withheld'}}
  with r:
   if r.headers.get('Mcp-Session-Id'):self.headers['Mcp-Session-Id']=r.headers['Mcp-Session-Id']
   raw=r.read(2*1024*1024+1)
   if len(raw)>2*1024*1024:return r.code,{'error':{'code':'response_limit'}}
   if not raw:return r.code,{}
   try:b=json.loads(raw)
   except (ValueError,UnicodeError):
    try:
     data=[json.loads(line[5:].strip()) for line in raw.decode().splitlines() if line.startswith('data:')]
     b=next((x for x in data if x.get('id')==body.get('id')),{'error':{'code':'unrecognized_sse'}})
    except (ValueError,UnicodeError):b={'error':{'code':'non_json_response','bytes':len(raw)}}
   return r.code,b
 def initialize(self):
  s,b=self.rpc('initialize',{'protocolVersion':'2024-11-05','capabilities':{},'clientInfo':{'name':'ddot-poc-readback','version':'1.0'}})
  if s==200 and 'result' in b:
   self.headers['MCP-Protocol-Version']=b['result'].get('protocolVersion','2024-11-05')
   self.rpc('notifications/initialized')
  return s,b
