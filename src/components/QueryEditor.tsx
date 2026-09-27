import React, { ChangeEvent } from 'react';
import { InlineField, Input, MultiSelect, Select, Stack, TextArea } from '@grafana/ui';
import { QueryEditorProps, SelectableValue } from '@grafana/data';
import { DataSource } from '../datasource';
import {
  MARKETPLACE_CATEGORY_OPTIONS,
  MARKETPLACE_OPTIONS,
  MARKETPLACE_ROUTES,
  MAX_QUERY_LIMIT,
  ALERT_METRIC_OPTIONS,
  AlertMetric,
  MarketplaceDataSourceOptions,
  MarketplaceCategoryId,
  MarketplaceQueryType,
  MarketplaceQuery,
  MarketplaceSelection,
  QUERY_TYPE_OPTIONS,
  RequestMethod,
} from '../types';

export type Props = QueryEditorProps<DataSource, MarketplaceQuery, MarketplaceDataSourceOptions>;
const MARKETPLACE_SELECT_OPTIONS: Array<SelectableValue<MarketplaceSelection>> = MARKETPLACE_OPTIONS.map(
  ({ label, value }) => ({ label, value })
);
const QUERY_TYPE_SELECT_OPTIONS: Array<SelectableValue<MarketplaceQueryType>> = QUERY_TYPE_OPTIONS.map(
  ({ label, value }) => ({ label, value })
);
const ALERT_METRIC_SELECT_OPTIONS: Array<SelectableValue<AlertMetric>> = ALERT_METRIC_OPTIONS.map(
  ({ label, value }) => ({ label, value })
);
const METHODS = [
  { label: 'GET', value: 'GET' as RequestMethod },
  { label: 'POST', value: 'POST' as RequestMethod },
];

export function QueryEditor({ query, onChange, onRunQuery }: Props) {
  const isMetricsQuery = query.marketplace === 'metrics';
  const isAlertQuery = isMetricsQuery && query.queryType === 'alert';
  const selectedMarketplace = query.selectedMarketplace ?? 'both';
  const categories = query.categories ?? [];
  const categoryOptions: Array<SelectableValue<MarketplaceCategoryId>> =
    MARKETPLACE_CATEGORY_OPTIONS[selectedMarketplace].map(({ label, value }) => ({ label, value }));

  const update = (values: Partial<MarketplaceQuery>, runQuery = false) => {
    onChange({ ...query, ...values });
    if (runQuery) {
      onRunQuery();
    }
  };

  const onMarketplaceChange = (option: SelectableValue<MarketplaceSelection>) => {
    if (!option.value) {
      return;
    }
    const availableCategories = new Set(
      MARKETPLACE_CATEGORY_OPTIONS[option.value].map((category) => category.value)
    );
    update(
      {
        selectedMarketplace: option.value,
        categories: categories.filter((category) => availableCategories.has(category)),
      },
      true
    );
  };

  const onLimitChange = (event: ChangeEvent<HTMLInputElement>) => {
    const value = event.target.value;

    if (value.trim() === '') {
      update({ limit: undefined });
      return;
    }

    const parsedLimit = Number(value);
    if (!Number.isSafeInteger(parsedLimit) || parsedLimit < 1 || parsedLimit > MAX_QUERY_LIMIT) {
      onChange({ ...query });
      return;
    }

    update({ limit: parsedLimit }, true);
  };

  const onPathChange = (event: ChangeEvent<HTMLInputElement>) => update({ path: event.target.value }, true);
  const onBodyChange = (event: ChangeEvent<HTMLTextAreaElement>) => update({ body: event.target.value }, true);

  return (
    <Stack gap={1}>
      <InlineField label="Marketplace" labelWidth={20}>
        <Select<MarketplaceSelection>
          aria-label="Marketplace"
          options={MARKETPLACE_SELECT_OPTIONS}
          value={MARKETPLACE_SELECT_OPTIONS.find((option) => option.value === selectedMarketplace)}
          onChange={onMarketplaceChange}
          width={40}
        />
      </InlineField>
      <InlineField label="Query type" labelWidth={20}>
        <Select<MarketplaceQueryType>
          aria-label="Query type"
          options={QUERY_TYPE_SELECT_OPTIONS}
          value={QUERY_TYPE_SELECT_OPTIONS.find((option) => option.value === (query.queryType ?? 'profitability'))}
          onChange={(option) => {
            if (option.value) {
              update({ queryType: option.value }, true);
            }
          }}
          width={40}
        />
      </InlineField>
      {isAlertQuery && (
        <InlineField
          label="Alert metric"
          labelWidth={20}
          tooltip="Returns only known numeric values as labeled time series. Unsupported or missing source data produces no data, never a zero."
        >
          <Select<AlertMetric>
            aria-label="Alert metric"
            options={ALERT_METRIC_SELECT_OPTIONS}
            value={ALERT_METRIC_SELECT_OPTIONS.find((option) => option.value === query.alertMetric)}
            onChange={(option) => {
              if (option.value) {
                update({ alertMetric: option.value }, true);
              }
            }}
            width={40}
          />
        </InlineField>
      )}
      <InlineField
        label="Categories"
        labelWidth={20}
        tooltip="Static category groups are namespaced by marketplace, so Wildberries and Ozon categories remain distinct."
      >
        <MultiSelect
          aria-label="Categories"
          options={categoryOptions}
          value={categoryOptions.filter((option) => option.value !== undefined && categories.includes(option.value))}
          onChange={(options) =>
            update(
              { categories: (options ?? []).flatMap((option) => (option.value ? [option.value] : [])) },
              true
            )
          }
          width={40}
        />
      </InlineField>
      <InlineField
        label="Time range"
        labelWidth={20}
        tooltip="The query uses the time range selected in Grafana's dashboard toolbar."
      >
        <Input aria-label="Grafana dashboard time range" disabled value="Uses dashboard time range" width={40} />
      </InlineField>
      <InlineField
        label="Maximum records"
        labelWidth={20}
        tooltip={`Maximum number of records to return (1–${MAX_QUERY_LIMIT.toLocaleString()}). Leave blank to omit the limit.`}
      >
        <Input
          aria-label="Maximum records"
          type="number"
          min={1}
          max={MAX_QUERY_LIMIT}
          step={1}
          value={query.limit?.toString() ?? ''}
          onChange={onLimitChange}
          width={20}
        />
      </InlineField>
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
              placeholder="/api/..."
              width={60}
            />
          </InlineField>
          <InlineField label="JSON body" labelWidth={20} tooltip="Used for POST requests.">
            <TextArea
              aria-label="JSON body"
              value={query.body ?? ''}
              onChange={onBodyChange}
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
