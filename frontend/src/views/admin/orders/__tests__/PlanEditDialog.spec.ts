import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import PlanEditDialog from '../PlanEditDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => {
      if (key === 'payment.admin.subscriptionCnyPayPreview') return `preview ${params?.amount}`
      if (key === 'payment.admin.subscriptionCnyPayPreviewWithFee') return `fee ${params?.feeRate} ${params?.total}`
      return key
    },
  }),
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}))

vi.mock('@/api/admin/payment', () => ({
  adminPaymentAPI: {
    createPlan: vi.fn(),
    updatePlan: vi.fn(),
  },
}))

function mountDialog(paymentConfig: Record<string, unknown> | null) {
  return mount(PlanEditDialog, {
    props: {
      show: true,
      plan: null,
      groups: [],
      paymentConfig,
    },
    global: {
      stubs: {
        BaseDialog: {
          props: ['show'],
          template: '<div v-if="show"><slot /><slot name="footer" /></div>',
        },
        Select: true,
        Icon: true,
        GroupBadge: true,
        PlatformIcon: true,
      },
    },
  })
}

describe('PlanEditDialog subscription CNY payment preview', () => {
  it('shows CNY channel charge using the configured subscription rate and fee', async () => {
    const wrapper = mountDialog({
      subscription_usd_to_cny_rate: 7.15,
      recharge_fee_rate: 2.5,
    })

    await wrapper.find('input[type="number"]').setValue('9.99')

    expect(wrapper.text()).toContain('preview')
    expect(wrapper.text()).toContain('¥71.43')
    expect(wrapper.text()).toContain('fee 2.5')
    expect(wrapper.text()).toContain('¥73.22')
  })

  it('hides the preview when the subscription rate is not configured', async () => {
    const wrapper = mountDialog({
      subscription_usd_to_cny_rate: 0,
      recharge_fee_rate: 2.5,
    })

    await wrapper.find('input[type="number"]').setValue('9.99')

    expect(wrapper.text()).not.toContain('preview')
    expect(wrapper.text()).not.toContain('¥71.43')
  })
})

describe('PlanEditDialog mixed platform groups', () => {
  it('lists non-openai groups and warns when image groups are selected', async () => {
    const wrapper = mount(PlanEditDialog, {
      props: {
        show: true,
        plan: null,
        groups: [
          { id: 1, name: 'GPT Line', platform: 'openai', rate_multiplier: 1, allow_image_generation: false },
          { id: 2, name: 'Claude Line', platform: 'anthropic', rate_multiplier: 1, allow_image_generation: false },
          { id: 3, name: 'Gemini Image', platform: 'gemini', rate_multiplier: 1, allow_image_generation: true },
        ] as any,
        paymentConfig: null,
      },
      global: {
        stubs: {
          BaseDialog: {
            props: ['show'],
            template: '<div v-if="show"><slot /><slot name="footer" /></div>',
          },
          Select: true,
          Icon: true,
          GroupBadge: true,
          PlatformIcon: true,
        },
      },
    })

    expect(wrapper.text()).toContain('GPT Line')
    expect(wrapper.text()).toContain('Claude Line')
    expect(wrapper.text()).toContain('Gemini Image')
    expect(wrapper.text()).not.toContain('payment.admin.imageGroupsWarning')

    const boxes = wrapper.findAll('input[type="checkbox"]')
    await boxes[2].setValue(true)
    expect(wrapper.text()).toContain('payment.admin.imageGroupsWarning')
  })
})
