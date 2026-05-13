import { AccountForm } from './components/AccountForm';
import type { PluginFrontendModule } from '@doudou-start/airgate-theme/plugin';
import { ClaudeIcon } from './components/ClaudeIcon';

const plugin: PluginFrontendModule = {
  accountCreate: AccountForm,
  accountEdit: AccountForm,
  platformIcon: ClaudeIcon,
};

export default plugin;
