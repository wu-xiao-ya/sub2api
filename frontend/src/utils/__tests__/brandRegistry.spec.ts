import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createHash } from 'node:crypto'
import { compileStyle } from 'vue/compiler-sfc'
import { brandAssets, resolveModelBrand, resolvePlatformBrand } from '../brandRegistry'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
import ProviderIcon from '@/components/user/monitor/ProviderIcon.vue'
import sources from '@/assets/brands/sources.json'

describe('shared brand resources', () => {
  it('keeps dark icon backing scoped to brand marks, never the page root', () => {
    const source = readFileSync(resolve(__dirname, '../../components/common/BrandIcon.vue'), 'utf8')
    const css = source.match(/<style scoped>([\s\S]*?)<\/style>/)?.[1] || ''
    const result = compileStyle({ source: css, filename: 'BrandIcon.vue', id: 'data-v-brand', scoped: true })
    expect(result.errors).toEqual([])
    expect(result.code).toContain('.dark .brand-icon:has(img)')
    expect(result.code).not.toMatch(/\.dark\s*\{/)
  })
  it.each(['openai','anthropic','gemini','grok','antigravity','deepseek','kimi','glm','qwen','minimax','mimo','hunyuan'])('uses the same %s mark in platform and monitoring views', (brand) => {
    const platform = mount(PlatformIcon, { props: { platform: brand } })
    const provider = mount(ProviderIcon, { props: { provider: brand } })
    expect(platform.get('img').attributes('src')).toBe(provider.get('img').attributes('src'))
    expect(platform.attributes('data-brand')).toBe(brand)
  })
  it.each([
    ['proxy/DeepSeek-v4-pro','deepseek'], ['Kimi-K3','kimi'], ['vendor/k3-256k','kimi'],
    ['GLM-5.3-Flash','glm'], ['Qwen-3.8-Max','qwen'], ['MiniMax-M3','minimax'],
    ['Xiaomi/MiMo-V2.5','mimo'], ['hunyuan-turbo','hunyuan'], ['gpt-6-astra','openai'],
    ['claude-opus-5','anthropic'], ['gemini-3.8-flash','gemini'], ['grok-4.6','grok'],
    ['custom-adapter',''], ['my-unknown-model',''],
  ])('resolves the model family of %s', (model, brand) => {
    expect(resolveModelBrand(model)).toBe(brand || null)
  })
  it('normalizes platform aliases without confusing compatibility and branding', () => {
    expect(resolvePlatformBrand(' KIMI ')).toBe('kimi')
    expect(resolvePlatformBrand('zhipu')).toBe('glm')
    expect(resolvePlatformBrand('openai-compatible')).toBeNull()
    expect(resolvePlatformBrand('toString')).toBeNull()
    expect(resolvePlatformBrand('constructor')).toBeNull()
    expect(resolveModelBrand('deepseek-chat')).not.toBe('openai')
  })
  it.each([
    ['opus-4.8', 'anthropic'], ['opus-5', 'anthropic'],
    ['sonnet-4.6', 'anthropic'], ['sonnet-5', 'anthropic'],
    ['haiku-4.5', 'anthropic'], ['fable-5.1', 'anthropic'],
    ['vendor/OPUS-5', 'anthropic'], [' Sonnet-5 ', 'anthropic'],
    ['hy3', 'hunyuan'], ['vendor/HY3', 'hunyuan'],
    ['hy3-preview', 'hunyuan'], ['hy3.0', 'hunyuan'],
    ['hy-3', 'hunyuan'], ['hunyuan-hy3', 'hunyuan'],
  ])('renders the correct bundled image for short model alias %s', (model, brand) => {
    expect(resolveModelBrand(model)).toBe(brand)
    const icon = mount(ModelIcon, { props: { model } })
    const platform = mount(PlatformIcon, { props: { platform: brand } })
    expect(icon.attributes('data-brand')).toBe(brand)
    expect(icon.get('img').attributes('src')).toBe(platform.get('img').attributes('src'))
  })
  it.each(['hybrid-chat', 'hyper-model', 'hy', 'hy3custom', 'opuslike', 'sonnetizer', 'my-opus-5'])('does not guess a brand for unrelated model %s', (model) => {
    expect(resolveModelBrand(model)).toBeNull()
  })
  it('preserves legacy CSS string sizes and numeric sizes', () => {
    expect(mount(ModelIcon, { props: { model:'kimi-k3', size:'16px' } }).attributes('style')).toContain('16px')
    expect(mount(ModelIcon, { props: { model:'mimo-v2', size:24 } }).attributes('style')).toContain('24px')
    expect(mount(ModelIcon, { props: { model:'private-model' } }).find('img').exists()).toBe(false)
  })
  it('ships every resource locally with an integrity record and no active SVG content', () => {
    expect(sources.assets.map(x => x.brand).sort()).toEqual(Object.keys(brandAssets).sort())
    for (const entry of sources.assets) {
      const path = fileURLToPath(new URL('../../assets/brands/' + (entry.file || entry.brand + '.svg'), import.meta.url))
      const bytes = readFileSync(path)
      const raster = entry.file?.endsWith('.png')
      expect(createHash('sha256').update(raster ? bytes : bytes.toString().replace(/\r\n/g, '\n').trim()).digest('hex')).toBe(entry.sha256)
      if (raster) {
        expect(bytes.subarray(0, 8).toString('hex')).toBe('89504e470d0a1a0a')
        continue
      }
      const doc = new DOMParser().parseFromString(bytes.toString(), 'image/svg+xml')
      expect(doc.querySelector('parsererror,script,foreignObject,image,use,iframe')).toBeNull()
      expect(doc.documentElement.getAttribute('viewBox')).toBeTruthy()
      for (const node of doc.querySelectorAll('*')) {
        for (const attr of node.attributes) {
          expect(attr.name).not.toMatch(/^on|href/i)
          if (/url\(/.test(attr.value)) expect(attr.value).toMatch(/^url\(#[\w-]+\)$/)
        }
      }
    }
  })
})
