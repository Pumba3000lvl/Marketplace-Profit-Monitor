import { DataSourcePlugin } from '@grafana/data';
import { DataSource } from './datasource';
import { ConfigEditor } from './components/ConfigEditor';
import { QueryEditor } from './components/QueryEditor';
import { MarketplaceQuery, MarketplaceDataSourceOptions } from './types';

export {
  QueryStateMessage,
  QueryStateMessageType,
  queryStateFromHttpStatus,
} from './components/QueryStateMessage';

export const plugin = new DataSourcePlugin<DataSource, MarketplaceQuery, MarketplaceDataSourceOptions>(DataSource)
  .setConfigEditor(ConfigEditor)
  .setQueryEditor(QueryEditor);
