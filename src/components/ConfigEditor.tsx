import React, { FormEvent } from 'react';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { InlineField, SecretInput } from '@grafana/ui';
import { MarketplaceDataSourceOptions, MarketplaceSecureJsonData } from '../types';

interface Props
  extends DataSourcePluginOptionsEditorProps<MarketplaceDataSourceOptions, MarketplaceSecureJsonData> {}

export function ConfigEditor({ options, onOptionsChange }: Props) {
  const { secureJsonData, secureJsonFields } = options;

  const setSecret = (key: keyof MarketplaceSecureJsonData, event: FormEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      secureJsonData: {
        ...secureJsonData,
        [key]: event.currentTarget.value,
      },
    });
  };

  const resetSecret = (key: keyof MarketplaceSecureJsonData) => {
    onOptionsChange({
      ...options,
      secureJsonFields: {
        ...secureJsonFields,
        [key]: false,
      },
      secureJsonData: {
        ...secureJsonData,
        [key]: '',
      },
    });
  };

  return (
    <>
      <InlineField label="Wildberries API token" labelWidth={24} tooltip="Stored encrypted by Grafana.">
        <SecretInput
          isConfigured={secureJsonFields?.wildberriesToken ?? false}
          value={secureJsonData?.wildberriesToken ?? ''}
          placeholder="Enter Wildberries token"
          width={40}
          onChange={(event) => setSecret('wildberriesToken', event)}
          onReset={() => resetSecret('wildberriesToken')}
        />
      </InlineField>
      <InlineField label="Ozon Client-Id" labelWidth={24} tooltip="Stored encrypted by Grafana.">
        <SecretInput
          isConfigured={secureJsonFields?.ozonClientId ?? false}
          value={secureJsonData?.ozonClientId ?? ''}
          placeholder="Enter Ozon Client-Id"
          width={40}
          onChange={(event) => setSecret('ozonClientId', event)}
          onReset={() => resetSecret('ozonClientId')}
        />
      </InlineField>
      <InlineField label="Ozon API key" labelWidth={24} tooltip="Stored encrypted by Grafana.">
        <SecretInput
          isConfigured={secureJsonFields?.ozonApiKey ?? false}
          value={secureJsonData?.ozonApiKey ?? ''}
          placeholder="Enter Ozon API key"
          width={40}
          onChange={(event) => setSecret('ozonApiKey', event)}
          onReset={() => resetSecret('ozonApiKey')}
        />
      </InlineField>
    </>
  );
}
