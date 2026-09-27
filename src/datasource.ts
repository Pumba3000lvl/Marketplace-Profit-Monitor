import { CoreApp, DataQueryRequest, DataSourceInstanceSettings, ScopedVars } from '@grafana/data';
import { DataSourceWithBackend, getTemplateSrv } from '@grafana/runtime';
import {
  DEFAULT_QUERY,
  MarketplaceDataSourceOptions,
  MarketplaceQuery,
} from './types';

export class DataSource extends DataSourceWithBackend<MarketplaceQuery, MarketplaceDataSourceOptions> {
  constructor(instanceSettings: DataSourceInstanceSettings<MarketplaceDataSourceOptions>) {
    super(instanceSettings);
  }

  getDefaultQuery(_app: CoreApp): Partial<MarketplaceQuery> {
    return DEFAULT_QUERY;
  }

  applyTemplateVariables(query: MarketplaceQuery, scopedVars: ScopedVars): MarketplaceQuery {
    return {
      ...query,
      path: getTemplateSrv().replace(query.path, scopedVars),
      body: query.body ? getTemplateSrv().replace(query.body, scopedVars) : query.body,
    };
  }

  filterQuery(query: MarketplaceQuery, _options?: DataQueryRequest<MarketplaceQuery>): boolean {
    return Boolean(query.marketplace && query.path?.trim() && query.method);
  }
}
