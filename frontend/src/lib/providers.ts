// The provider kinds the UI can configure. Ids match the Go config provider
// types; `labelKey` resolves through i18n under `onboarding.*`. Both the
// onboarding wizard and the provider settings read this one list.

export interface ProviderTypeSpec {
  id: string;
  /** Brand name stored as the provider's name when created from onboarding. */
  displayName: string;
  labelKey: string;
  baseUrl: string;
  needKey: boolean;
  defaultModels: string;
}

export const PROVIDER_TYPES: readonly ProviderTypeSpec[] = [
  {
    id: 'openai',
    displayName: 'OpenAI',
    labelKey: 'onboarding.providerTypeOpenAI',
    baseUrl: 'https://api.openai.com/v1',
    needKey: true,
    defaultModels: 'gpt-4o, gpt-4o-mini, gpt-4.1',
  },
  {
    id: 'anthropic',
    displayName: 'Anthropic',
    labelKey: 'onboarding.providerTypeAnthropic',
    baseUrl: 'https://api.anthropic.com',
    needKey: true,
    defaultModels: 'claude-sonnet-4-6, claude-haiku-4-5',
  },
  {
    id: 'ollama',
    displayName: 'Ollama',
    labelKey: 'onboarding.providerTypeOllama',
    baseUrl: 'http://localhost:11434',
    needKey: false,
    defaultModels: 'llama3.2, mistral, gemma3',
  },
];

export const PROVIDER_TYPE_IDS: readonly string[] = PROVIDER_TYPES.map(p => p.id);

/** Spec for a provider type id, falling back to the first entry. */
export function providerSpec(id: string): ProviderTypeSpec {
  return PROVIDER_TYPES.find(p => p.id === id) ?? PROVIDER_TYPES[0];
}
