import React, { ChangeEvent } from 'react';
import { InlineField, Input, Select, Stack, TextArea } from '@grafana/ui';
import { QueryEditorProps } from '@grafana/data';
import { DataSource } from '../datasource';
import {
  MARKETPLACE_ROUTES,
  MarketplaceDataSourceOptions,
  MarketplaceQuery,
  RequestMethod,
} from '../types';

type Props = QueryEditorProps<DataSource, MarketplaceQuery, MarketplaceDataSourceOptions>;
const METHODS = [
  { label: 'GET', value: 'GET' as RequestMethod },
  { label: 'POST', value: 'POST' as RequestMethod },
];

export function QueryEditor({ query, onChange, onRunQuery }: Props) {
  const isMetricsQuery = query.marketplace === 'metrics';
  const update = (values: Partial<MarketplaceQuery>, runQuery = false) => {
    onChange({ ...query, ...values });
    if (runQuery) {
      onRunQuery();
    }
  };

  const onPathChange = (event: ChangeEvent<HTMLInputElement>) => update({ path: event.target.value });
  const onBodyChange = (event: ChangeEvent<HTMLTextAreaElement>) => update({ body: event.target.value });

  return (
    <Stack gap={1}>
      <InlineField label="Marketplace API" labelWidth={20}>
        <Select
          aria-label="Marketplace API"
          options={MARKETPLACE_ROUTES}
          value={MARKETPLACE_ROUTES.find((route) => route.value === query.marketplace)}
          onChange={(option) => {
            if (option.value) {
              update({ marketplace: option.value }, true);
            }
          }}
          width={40}
        />
      </InlineField>
      {!isMetricsQuery && (
        <>
          <InlineField label="Method" labelWidth={20}>
            <Select
              aria-label="HTTP method"
              options={METHODS}
              value={METHODS.find((method) => method.value === query.method)}
              onChange={(option) => {
                if (option.value) {
                  update({ method: option.value }, true);
                }
              }}
              width={12}
            />
          </InlineField>
          <InlineField label="API path" labelWidth={20} tooltip="Relative path on the selected API host; must start with /.">
            <Input
              aria-label="API path"
              value={query.path ?? ''}
              onChange={onPathChange}
              onBlur={() => onRunQuery()}
              placeholder="/api/..."
              width={60}
            />
          </InlineField>
          <InlineField label="JSON body" labelWidth={20} tooltip="Used for POST requests.">
            <TextArea
              aria-label="JSON body"
              value={query.body ?? ''}
              onChange={onBodyChange}
              onBlur={() => onRunQuery()}
              placeholder='{"key":"value"}'
              rows={4}
              width={60}
            />
          </InlineField>
        </>
      )}
    </Stack>
  );
}
