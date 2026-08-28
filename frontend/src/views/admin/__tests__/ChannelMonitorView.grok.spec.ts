import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import MonitorFormDialog from '@/components/admin/monitor/MonitorFormDialog.vue'
import {
  API_MODE_CHAT_COMPLETIONS,
  API_MODE_RESPONSES,
  DEFAULT_GROK_ENDPOINT,
  DEFAULT_GROK_MODEL,
  PROVIDERS,
  PROVIDER_DEEPSEEK,
  PROVIDER_GLM,
  PROVIDER_GROK,
  PROVIDER_KIMI,
} from '@/constants/channelMonitor'

const { createMonitor, listTemplates } = vi.hoisted(() => ({
  createMonitor: vi.fn(),
  listTemplates: vi.fn(),
}))


vi.mock('@/utils/featureFlags', () => ({
  isChannelMonitorV1Mode: () => true,
  isChannelMonitorV2Mode: () => false,
  getChannelMonitorMode: () => 'v1' as const,
}))

vi.mock('@/features/channel-monitor-v2/MonitorSettingsPanel.vue', () => ({
  default: { name: 'MonitorSettingsPanel', template: '<div data-testid="v2-settings" />' },
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channelMonitor: {
      create: createMonitor,
      update: vi.fn(),
    },
    channelMonitorTemplate: {
      list: listTemplates,
    },
    groups: {
      getByPlatform: vi.fn().mockResolvedValue([]),
    },
  },
}))

vi.mock('@/api/keys', () => ({
  keysAPI: { list: vi.fn() },
}))

vi.mock('@/api/groups', () => ({
  userGroupsAPI: { getUserGroupRates: vi.fn() },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null,
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const BaseDialogStub = defineComponent({
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

function mountDialog() {
  return mount(MonitorFormDialog, {
    props: { show: true, monitor: null },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Toggle: true,
        Select: true,
        ModelTagInput: true,
        MonitorKeyPickerDialog: true,
        MonitorAdvancedRequestConfig: true,
      },
    },
  })
}

describe('channel monitor provider options', () => {
  beforeEach(() => {
    listTemplates.mockReset().mockResolvedValue({ items: [] })
    createMonitor.mockReset().mockResolvedValue({})
  })

  it('offers Grok in the responsive provider grid and prefills its official defaults', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    expect(PROVIDERS).toContain(PROVIDER_GROK)
    const providerButtons = wrapper.findAll('[data-testid^="monitor-provider-"]')
    expect(providerButtons).toHaveLength(12)
    expect(providerButtons[0].element.parentElement?.className).toContain('grid-cols-2')
    expect(providerButtons[0].element.parentElement?.className).toContain('sm:grid-cols-4')

    const grokButton = wrapper.get('[data-testid="monitor-provider-grok"]')
    expect(grokButton.find('svg').exists()).toBe(true)
    expect(grokButton.text()).toContain('monitorCommon.providers.grok')
    await grokButton.trigger('click')
    expect(grokButton.classes().join(' ')).toContain('zinc')

    const endpoint = wrapper.get('[data-testid="monitor-endpoint"]')
    const model = wrapper.get('[data-testid="monitor-primary-model"]')
    expect((endpoint.element as HTMLInputElement).value).toBe(DEFAULT_GROK_ENDPOINT)
    expect((model.element as HTMLInputElement).value).toBe(DEFAULT_GROK_MODEL)

    await wrapper.get('[data-testid="monitor-provider-anthropic"]').trigger('click')
    expect((endpoint.element as HTMLInputElement).value).toBe('')
    expect((model.element as HTMLInputElement).value).toBe('')

    await grokButton.trigger('click')
    await endpoint.setValue('https://gateway.example.com')
    await model.setValue('grok-custom')
    await wrapper.get('[data-testid="monitor-provider-openai"]').trigger('click')
    expect((endpoint.element as HTMLInputElement).value).toBe('https://gateway.example.com')
    expect((model.element as HTMLInputElement).value).toBe('grok-custom')
  })

  it('prefills only empty Grok fields and preserves existing provider values', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const endpoint = wrapper.get('[data-testid="monitor-endpoint"]')
    const model = wrapper.get('[data-testid="monitor-primary-model"]')
    const grokButton = wrapper.get('[data-testid="monitor-provider-grok"]')
    const anthropicButton = wrapper.get('[data-testid="monitor-provider-anthropic"]')

    await endpoint.setValue('https://gateway.example.com')
    await grokButton.trigger('click')
    expect((endpoint.element as HTMLInputElement).value).toBe('https://gateway.example.com')
    expect((model.element as HTMLInputElement).value).toBe(DEFAULT_GROK_MODEL)

    await anthropicButton.trigger('click')
    expect((endpoint.element as HTMLInputElement).value).toBe('https://gateway.example.com')
    expect((model.element as HTMLInputElement).value).toBe('')

    await endpoint.setValue('')
    await model.setValue('grok-custom')
    await grokButton.trigger('click')
    expect((endpoint.element as HTMLInputElement).value).toBe(DEFAULT_GROK_ENDPOINT)
    expect((model.element as HTMLInputElement).value).toBe('grok-custom')
  })

  it.each([
    [PROVIDER_DEEPSEEK, 'deepseek'],
    [PROVIDER_KIMI, 'kimi'],
    [PROVIDER_GLM, 'glm'],
  ] as const)(
    'offers %s with an icon and forces chat completions on create',
    async (provider, providerName) => {
      const wrapper = mountDialog()
      await flushPromises()

      expect(PROVIDERS).toContain(provider)
      const providerButton = wrapper.get(`[data-testid="monitor-provider-${provider}"]`)
      expect(providerButton.find('svg').exists()).toBe(true)
      expect(providerButton.text()).toContain(`monitorCommon.providers.${providerName}`)

      await wrapper.get('[data-testid="monitor-provider-openai"]').trigger('click')
      expect(wrapper.text()).toContain('admin.channelMonitor.form.apiModeResponses')
      await providerButton.trigger('click')
      expect(wrapper.text()).not.toContain('admin.channelMonitor.form.apiModeResponses')

      await wrapper.get('[data-testid="monitor-endpoint"]').setValue(`https://${provider}.example.com`)
      await wrapper.get('[data-testid="monitor-primary-model"]').setValue(`${provider}-chat`)
      await wrapper.get('input[type="text"]').setValue(`${provider} monitor`)
      await wrapper.get('form').trigger('submit')

      expect(createMonitor).toHaveBeenCalledWith(expect.objectContaining({
        provider,
        api_mode: API_MODE_CHAT_COMPLETIONS,
      }))
      expect(createMonitor).not.toHaveBeenCalledWith(expect.objectContaining({
        api_mode: API_MODE_RESPONSES,
      }))
    },
  )
})
