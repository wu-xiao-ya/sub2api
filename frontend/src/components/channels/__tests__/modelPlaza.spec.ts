import { describe, expect, it } from 'vitest'
import {
  buildModelPlazaItems,
  effectiveGroupRate,
  lowestGroupRate,
} from '../modelPlaza'

describe('model plaza data shaping', () => {
  const group = {
    id: 7,
    name: 'GPT Plus',
    platform: 'openai',
    subscription_type: 'standard',
    rate_multiplier: 0.2,
    peak_rate_enabled: false,
    peak_start: '',
    peak_end: '',
    peak_rate_multiplier: 0,
    is_exclusive: false,
  }

  it('keeps identically named models from different channels distinct', () => {
    const items = buildModelPlazaItems([
      {
        name: 'OpenAI Main',
        description: 'Default route',
        platforms: [
          {
            platform: 'openai',
            groups: [group],
            supported_models: [
              {
                name: 'gpt-5.6-sol',
                platform: 'openai',
                pricing: null,
              },
            ],
          },
        ],
      },
      {
        name: 'OpenAI Backup',
        description: 'Fallback route',
        platforms: [
          {
            platform: 'openai',
            groups: [group],
            supported_models: [
              {
                name: 'gpt-5.6-sol',
                platform: 'openai',
                pricing: null,
              },
            ],
          },
        ],
      },
    ])

    expect(items).toHaveLength(2)
    expect(items.map(item => item.key)).toEqual([
      'OpenAI Main\u0000openai\u0000gpt-5.6-sol',
      'OpenAI Backup\u0000openai\u0000gpt-5.6-sol',
    ])
  })

  it('uses a user-specific multiplier when computing the lowest visible rate', () => {
    const higherRateGroup = { ...group, id: 8, rate_multiplier: 0.4 }
    const rates = { 7: 0.18 }

    expect(effectiveGroupRate(group, rates)).toBe(0.18)
    expect(lowestGroupRate([group, higherRateGroup], rates)).toBe(0.18)
    expect(lowestGroupRate([], rates)).toBeNull()
  })
})
