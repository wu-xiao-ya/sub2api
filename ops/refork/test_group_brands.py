import unittest
from group_brands import classify

class GroupBrandsTest(unittest.TestCase):
 def test_provenance_not_name_controls_backfill(self):
  for brand in ['qwen','hunyuan','mimo']:
   g={'name':'GPT','platform':'openai','upstream_provider':'','model_allowlist':{'models':['exact']}}
   p={'before_row':{'platform':brand},'after_row':{'platform':'openai','model_allowlist':g['model_allowlist']}}
   self.assertEqual((brand,'ready'),classify(g,p,[brand]))
   self.assertEqual((None,'already_applied'),classify(dict(g,upstream_provider=brand),p,[brand]))
   self.assertEqual((None,'missing_brand_provenance'),classify(g,None,[brand]))
 def test_changed_or_mixed_groups_are_not_guessed(self):
  g={'platform':'openai','upstream_provider':'','model_allowlist':{}}
  p={'before_row':{'platform':'qwen'},'after_row':{'platform':'openai','model_allowlist':{}}}
  for modified,accounts,reason in [(dict(g,upstream_provider='mimo'),['qwen'],'explicit_brand_conflict'),(dict(g,model_allowlist={'models':['new']}),['qwen'],'model_scope_changed'),(g,['qwen','mimo'],'mixed_account_brands'),(g,[None],'unknown_account_provenance')]:
   self.assertEqual((None,reason),classify(modified,p,accounts))

if __name__=='__main__':unittest.main()
