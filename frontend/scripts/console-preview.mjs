import http from 'node:http'
import { createServer as createViteServer } from 'vite'

// Local-only synthetic data. No production credentials or outbound gateway calls.
const user = { id: 999, email: 'preview@example.invalid', username: '设计预览', role: 'user', status: 'active', balance: 1286.54321, concurrency: 40, created_at: '2026-09-01T00:00:00Z' }
const platforms = ['openai','deepseek','kimi','glm','qwen','minimax','mimo','hunyuan','anthropic','gemini','grok','antigravity']
const names = ['GPT 专业线路','DeepSeek','Kimi','GLM 智谱','Qwen 千问','MiniMax','MiMo','腾讯混元','Claude','Gemini','Grok','Antigravity']
const modelNames = ['gpt-6-astra','deepseek-v4-pro','kimi-k3','glm-5.3-flash','qwen-3.8-max','MiniMax-M3','mimo-v2.5-pro','hunyuan-turbo','claude-opus-5','gemini-3.8-flash','grok-4.6','gemini-3.8-pro']
const groups = platforms.map((platform, i) => ({ id: i+1, name: names[i], platform, subscription_type: 'standard', rate_multiplier: .3, is_exclusive: false, status: 'active', peak_rate_enabled: false, peak_start: '', peak_end: '', peak_rate_multiplier: 1 }))
const pricing = { billing_mode:'token',input_price:0.000008,output_price:0.000028,cache_read_price:0.0000016,cache_write_price:null,image_input_price:null,image_output_price:null,per_request_price:null,intervals:[] }
const channels = [{ name:'星光精选', description:'', platforms:groups.map((g,i)=>({ platform:g.platform, groups:[g], supported_models:[{name:modelNames[i],platform:g.platform,pricing}] })) }]
let keys = groups.slice(0,7).map((g,i)=>({id:i+1,name:names[i]+' 开发环境',key:'sk-preview-not-valid-'+String(i).repeat(32),status:'active',group_id:g.id,group:g,user_id:999,created_at:new Date().toISOString(),quota:0,quota_used:0,ip_whitelist:[],ip_blacklist:[],expires_at:null}))
let subscriptions = [{id:1,name:'星光 Pro 月卡',tier_code:'pro',status:'active',starts_at:'2026-09-01T00:00:00Z',expires_at:'2026-10-01T00:00:00Z',concurrency_entitlement:20,lifetime_quota_usd:250,daily_quota_usd:30,weekly_quota_usd:100,monthly_quota_usd:250,lifetime_usage_usd:68.35,daily_usage_usd:8.92,weekly_usage_usd:38.41,monthly_usage_usd:68.35,balance_topup_enabled:false,billing_priority:'subscription',groups:groups.slice(0,4)}]
let preferences = { balance_topup_enabled:true }
const usage = modelNames.slice(0,7).map((model,i)=>({id:i+1,api_key_id:i+1,api_key:keys[i],group_id:i+1,group:groups[i],model,requested_model:model,billing_type:i===0?1:0,subscription_purchase_id:i===0?1:null,billing_source:i===0?'subscription':'balance',request_type:2,stream:true,input_tokens:8200+i*123,output_tokens:1380,cache_read_tokens:32000,cache_creation_tokens:0,input_cost:.0312,output_cost:.0414,cache_read_cost:.0021,total_cost:.08,actual_cost:.024,rate_multiplier:.3,duration_ms:8320,first_token_ms:826,latency_breakdown:i%3===0?{version:2,attempt_count:2,forward_start_ms:180,first_response_ms:450,first_event_ms:710,first_output_ms:740,first_character_ms:826,total_duration_ms:8320}:i%3===1?{first_response_ms:250,first_output_ms:600,first_character_ms:826,total_duration_ms:8320}:null,created_at:new Date(Date.now()-i*600000).toISOString(),service_tier:i===0?'priority':'standard',reasoning_effort:'high'}))
const stats = { total_api_keys:7,active_api_keys:7,total_requests:28340,total_input_tokens:8200000,total_output_tokens:630000,total_cache_read_tokens:34800000,total_cache_creation_tokens:82000,total_tokens:43712000,total_cost:543.821,total_actual_cost:163.1463,today_requests:1340,today_input_tokens:820000,today_output_tokens:63000,today_cache_read_tokens:3480000,today_cache_creation_tokens:8200,today_tokens:4371200,today_cost:54.3821,today_actual_cost:16.31463,average_duration_ms:8450,rpm:23,tpm:238000,by_platform:groups.slice(0,4).map(g=>({platform:g.platform,total_requests:7000,total_tokens:1200000,total_actual_cost:40.786575,today_actual_cost:4.0786575,today_requests:335,today_tokens:300000})) }
const trend = Array.from({length:10},(_,i)=>({date:new Date(Date.now()-(9-i)*86400000).toISOString().slice(0,10),requests:800+i*53,input_tokens:100000+i*20000,output_tokens:10000+i*1234,cache_read_tokens:500000+i*4567,cache_creation_tokens:1000,total_tokens:611000+i*30000,cost:25+i,actual_cost:7.5+i*.3}))
const models = modelNames.slice(0,5).map((model,i)=>({model,requests:800+i*53,total_tokens:611000+i*30000,input_tokens:100000,output_tokens:10000,cache_read_tokens:500000,cost:25+i,actual_cost:7.5+i*.3}))
const metric = {success_requests:1320,error_requests:20,request_count:1340,token_count:4371200,rpm:23,tpm:238000,error_rate:0.0149,cache_rate:0.832,cache_rate_numerator:832,cache_rate_denominator:1000,ttft:{sample_count:1320,p50_ms:826,p90_ms:1234,p95_ms:1650,avg_ms:900},duration:{sample_count:1320,p50_ms:8320,p95_ms:14500,avg_ms:9400}}
const health = {overall:'healthy',error_rate:'healthy',ttft:'healthy',cache:'healthy',score:96,minimum_sample:10}
const coverage = {requested_start:trend[0].date,coverage_start:trend[0].date,data_through:new Date().toISOString(),computed_at:new Date().toISOString(),aggregation_lag_seconds:3,coverage_complete:true,bucket_seconds:600}
const monitors = groups.slice(0,8).map((g,i)=>({id:i+1,name:g.name,provider:g.platform,api_mode:'chat_completions',group_name:g.name,account_group_id:g.id,primary_model:modelNames[i],primary_status:i===3?'degraded':'operational',primary_latency_ms:826+i*250,primary_ping_latency_ms:28,primary_source:i%2?'probe':'traffic',availability_7d:99.82,extra_models:[],timeline:Array.from({length:10},(_,n)=>({status:n===3&&i===3?'degraded':'operational',latency_ms:820+n*123,ping_latency_ms:28,checked_at:new Date(Date.now()-(9-n)*3600000).toISOString()}))}))
const paginated = items => ({items,total:items.length,page:1,page_size:20,pages:1})
function dataFor(path, method, body, params = new URLSearchParams()) {
  if(path==='/settings/public')return {site_name:'星光 AI · 本地预览',site_logo:'/logo.svg',version:'preview',registration_enabled:true,purchase_subscription_enabled:true,channel_monitor_enabled:true,channel_monitor_v2_enabled:true,api_base_url:'https://api.example.invalid',custom_endpoints:[],api_endpoint_probe_interval_seconds:0,feature_flags:{},backend_mode_enabled:false}
  if(path==='/auth/me'||path==='/user/profile')return user
  if(path==='/user/platform-quotas')return {platform_quotas:[]}
  if(path==='/groups/available')return groups
  if(path==='/groups/rates')return {}
  if(path==='/channels/available')return channels
  if(path==='/channels/performance'||path==='/channels/performance/detail') {
    const end = Date.now(), count = 24
    const performanceMetric = { first_character_ms: 826, duration_ms: 8320, output_tps: 48.6, success_rate: 98.4, sample_quality: 'adequate', coverage_status: 'complete' }
    const points = Array.from({length:count},(_,i)=>({at:new Date(end-(count-i)*3600000).toISOString(),...performanceMetric,first_character_ms:i===4?null:650+i*20,output_tps:i===4?null:44+i*.3,success_rate:i===4?null:96+i%5}))
    let items = channels.flatMap(ch=>ch.platforms.flatMap(p=>p.supported_models.map(m=>({key:ch.name+'\0'+p.platform+'\0'+m.name,model:m.name,platform:p.platform,...performanceMetric,groups:p.groups.map(g=>({id:g.id,name:g.name,...performanceMetric,trend:points})),trend:points}))))
    if(params.get('key')) items=items.filter(item=>item.key===params.get('key'))
    if(params.get('group_id')) items=items.map(item=>({...item,groups:item.groups.filter(group=>group.id===Number(params.get('group_id')))}))
    return {items,version:2,source:'user_requests',start:points[0].at,end:new Date(end).toISOString(),updated_at:new Date(end).toISOString(),coverage_start:points[0].at,coverage_end:new Date(end).toISOString(),incomplete_since:null}
  }
  if(path==='/subscriptions/shared')return subscriptions
  if(path==='/subscriptions/preferences')return preferences
  if(path==='/subscriptions/preferences/balance-topup'){preferences={balance_topup_enabled:body.enabled};return preferences}
  if(path.includes('/billing-priority')){subscriptions[0].billing_priority=body.priority;return {id:1,billing_priority:body.priority}}
  if(path.includes('/balance-topup')){subscriptions[0].balance_topup_enabled=body.enabled;return {id:1,balance_topup_enabled:body.enabled}}
  if(path==='/keys'&&method==='POST'){const group=groups.find(g=>g.id===Number(body.group_id))||groups[0];const key={...keys[0],...body,id:keys.length+1,group,key:'sk-preview-created-not-valid'};keys.push(key);return key}
  if(path==='/keys')return paginated(keys)
  if(path==='/usage/dashboard/api-keys-usage')return {stats:{}}
  if(path==='/usage')return paginated(usage)
  if(path.endsWith('/stats'))return {...stats,total:stats.total_requests,...metric}
  if(path.endsWith('/trend'))return {trend}
  if(path==='/usage/dashboard/models')return {models}
  if(path.includes('snapshot-v2'))return {trend,models,groups:groups.slice(0,4).map(g=>({...g,group_id:g.id,group_name:g.name,requests:335,cost:10,actual_cost:3,total_tokens:100000}))}
  if(path.includes('/keys/')&&path.includes('usage'))return {items:trend}
  if(path.includes('/usage/batch'))return {}
  if(path==='/channel-monitors')return {items:monitors}
  if(path.startsWith('/channel-monitors/')&&path.endsWith('/status')){const m=monitors[Number(path.split('/')[2])-1]||monitors[0];return {...m,models:[{model:m.primary_model,latest_status:m.primary_status,latest_latency_ms:m.primary_latency_ms,source:m.primary_source,availability_7d:99.82,availability_15d:99.9,availability_30d:99.8,avg_latency_7d_ms:826}]}}
  if(path.includes('channel-monitor-v2')){
    if(path.endsWith('/dimensions'))return {platforms:groups.map(g=>({value:g.platform,label:g.name,request_count:1340})),groups:groups.map(g=>({...g,request_count:1340})),models:modelNames.map(model=>({value:model,label:model,request_count:1340}))}
    if(path.endsWith('/snapshot'))return {config:{version:1,enabled:true,refresh_interval_seconds:60,platforms:groups.map(g=>({platform:g.platform,enabled:true,models:modelNames})),group_ids:groups.map(g=>g.id),health_thresholds:{minimum_sample:10,warning_error_rate:5,critical_error_rate:10,target_ttft_ms:1000,warning_ttft_ms:10000,critical_ttft_ms:20000,error_weight:.5,ttft_weight:.5}},coverage,metrics:metric,health,trend:trend.map(p=>({bucket_start:p.date,metrics:metric,health}))}
    if(path.endsWith('/matrix'))return {coverage,group_by:'platform_group_model',items:groups.slice(0,5).map((g,i)=>({platform:g.platform,group_id:g.id,group_name:g.name,model:modelNames[i],metrics:metric,health,buckets:trend.map(p=>({bucket_start:p.date,metrics:metric,health}))}))}
    return {coverage,items:[]}
  }
  if(path.includes('announcements'))return {items:[],unread_count:0}
  if(path.endsWith('/settings'))return {}
  if(path.includes('health'))return {status:'ok'}
  return []
}
const mock = http.createServer(async(req,res)=>{
  const path=new URL(req.url,'http://127.0.0.1').pathname.replace(/^\/api\/v1/,'')
  let raw='';for await(const chunk of req){raw+=chunk;if(raw.length>100000){res.writeHead(413);res.end();return}}
  let body={};try{body=JSON.parse(raw||'{}')}catch{res.writeHead(400);res.end();return}
  res.setHeader('Content-Type','application/json');res.setHeader('Cache-Control','no-store')
  if(req.url.startsWith('/v1/')){res.writeHead(403);res.end(JSON.stringify({error:'Preview has no upstream access'}));return}
  res.end(JSON.stringify({code:0,data:dataFor(path,req.method,body,new URL(req.url,'http://127.0.0.1').searchParams)}))
})
await new Promise((resolve,reject)=>{mock.once('error',reject);mock.listen(5195,'127.0.0.1',resolve)})
process.env.VITE_DEV_PROXY_TARGET='http://127.0.0.1:5195'
const vite=await createViteServer({server:{host:'127.0.0.1',port:5194,strictPort:true},plugins:[{name:'isolated-console-preview',configureServer(server){server.middlewares.use('/__preview',(req,res)=>{
  res.setHeader('Content-Type','text/html; charset=utf-8');res.setHeader('Cache-Control','no-store')
  res.end('<!doctype html><meta charset="utf-8"><script>localStorage.setItem("auth_token","preview-not-a-real-token");localStorage.setItem("auth_user",'+JSON.stringify(JSON.stringify(user))+');localStorage.setItem("locale","zh");for(let v=0;v<20;v++){localStorage.setItem("user_guide_999_user_"+v,"true");localStorage.setItem("user_guide_999_user_v"+v,"true")}location.replace("/dashboard")</script>')
})}}]})
await vite.listen()
console.log('Local synthetic preview: http://127.0.0.1:5194/__preview')
async function close(){await vite.close();mock.close();process.exit(0)}
process.on('SIGINT',close);process.on('SIGTERM',close)
