import openai from '@/assets/brands/openai.svg'
import anthropic from '@/assets/brands/anthropic.svg'
import gemini from '@/assets/brands/gemini.svg'
import grok from '@/assets/brands/grok.svg'
import antigravity from '@/assets/brands/antigravity.png'
import deepseek from '@/assets/brands/deepseek.svg'
import kimi from '@/assets/brands/kimi.svg'
import glm from '@/assets/brands/glm.png'
import qwen from '@/assets/brands/qwen.png'
import minimax from '@/assets/brands/minimax.svg'
import mimo from '@/assets/brands/mimo.svg'
import hunyuan from '@/assets/brands/hunyuan.svg'
import mistral from '@/assets/brands/mistral.svg'
import meta from '@/assets/brands/meta.svg'
import cohere from '@/assets/brands/cohere.svg'
import yi from '@/assets/brands/yi.svg'
import doubao from '@/assets/brands/doubao.svg'
import wenxin from '@/assets/brands/wenxin.svg'
import spark from '@/assets/brands/spark.svg'
import cloudflare from '@/assets/brands/cloudflare.svg'
import midjourney from '@/assets/brands/midjourney.svg'
import perplexity from '@/assets/brands/perplexity.svg'
import jina from '@/assets/brands/jina.svg'
import openrouter from '@/assets/brands/openrouter.svg'
import suno from '@/assets/brands/suno.svg'
import ollama from '@/assets/brands/ollama.svg'
import dify from '@/assets/brands/dify.svg'
import coze from '@/assets/brands/coze.svg'
import ai360 from '@/assets/brands/ai360.svg'

export const brandAssets = { openai, anthropic, gemini, grok, antigravity, deepseek, kimi, glm, qwen, minimax, mimo, hunyuan, mistral, meta, cohere, yi, doubao, wenxin, spark, cloudflare, midjourney, perplexity, jina, openrouter, suno, ollama, dify, coze, ai360 } as const
export type BrandKey = keyof typeof brandAssets
const aliases: Record<string, BrandKey> = {
  claude: 'anthropic', google: 'gemini', xai: 'grok', moonshot: 'kimi',
  zhipu: 'glm', zai: 'glm', 'z.ai': 'glm', xiaomimimo: 'mimo', 'xiaomi-mimo': 'mimo', tencent: 'hunyuan',
}
export function resolvePlatformBrand(platform?: string): BrandKey | null {
  const key = (platform || '').trim().toLowerCase()
  return Object.prototype.hasOwnProperty.call(brandAssets, key) ? key as BrandKey : Object.prototype.hasOwnProperty.call(aliases, key) ? aliases[key] : null
}

// Resolve the model family independently of its transport protocol or gateway.
export function resolveModelBrand(model: string): BrandKey | null {
  const name = (model || '').trim().toLowerCase().split('/').pop() || ''
  const families: [RegExp, BrandKey][] = [
    [/^(gpt(?:-|$)|chatgpt|o[134](?:-|$)|dall-e|whisper|tts-1|text-embedding|text-moderation|babbage|davinci|curie|ada(?:-|$))/, 'openai'],
    [/^(claude|(?:opus|sonnet|haiku|fable)(?:[-_. ]|$))/, 'anthropic'], [/^(gemini|gemma|learnlm|imagen|veo)/, 'gemini'],
    [/^deepseek/, 'deepseek'], [/^(kimi|moonshot|k3(?:-|$))/, 'kimi'],
    [/^(glm|chatglm|cogview|cogvideo)/, 'glm'], [/^(qwen|qwq|qvq)/, 'qwen'],
    [/^(minimax|abab)/, 'minimax'], [/^(mimo|xiaomi-mimo)/, 'mimo'], [/^(hunyuan|hy-?\d+(?:[.-]\d+)*(?:[-_. ]|$))/, 'hunyuan'],
    [/^grok/, 'grok'], [/^(mistral|mixtral|codestral|pixtral|voxtral|magistral)/, 'mistral'],
    [/^(meta-)?llama/, 'meta'], [/^(command|c4ai-|embed-)/, 'cohere'], [/^yi[- ]/, 'yi'],
    [/^doubao/, 'doubao'], [/^(ernie|wenxin)/, 'wenxin'], [/^spark/, 'spark'],
    [/^(mj_|midjourney)/, 'midjourney'], [/^(perplexity|pplx|sonar)/, 'perplexity'],
    [/^jina/, 'jina'], [/^openrouter/, 'openrouter'], [/^suno/, 'suno'], [/^ollama/, 'ollama'],
    [/^360/, 'ai360'], [/^dify/, 'dify'], [/^coze/, 'coze'],
  ]
  return families.find(([pattern]) => pattern.test(name))?.[1] ?? (model.toLowerCase().startsWith('@cf/') ? 'cloudflare' : null)
}
