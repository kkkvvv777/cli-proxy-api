// Common and canonical sections share these high-frequency field renderers.
// Only the active tab mounts, keeping FieldAnchor DOM IDs unique.

import { useTranslation } from 'react-i18next';
import type { ReactNode } from 'react';
import { Input } from '@/components/ui/Input';
import type { VisualConfigValues } from '@/types/visualConfig';
import { SPONSORS } from '../../sponsors';
import { ApiKeysCardEditor } from '../blocks/ApiKeysCardEditor';
import { FieldAnchor, FieldGroup, ToggleRow } from './FieldPrimitives';
import fieldStyles from './Field.module.scss';
import { EmployeeKeysEditor } from '@/features/company/EmployeeKeysEditor';
import { isCompanyMode } from '@/services/api/company';
import { useAuthStore, useConfigStore } from '@/stores';

export type SharedFieldProps = {
  values: VisualConfigValues;
  disabled: boolean;
  onChange: (patch: Partial<VisualConfigValues>) => void;
  /** Optional spacer for fields adjacent to the proxy URL. */
  topExtra?: ReactNode;
};

export function HostField({ values, disabled, onChange, topExtra }: SharedFieldProps) {
  const { t } = useTranslation();
  return (
    <FieldAnchor fieldId="host">
      <Input
        label={t('config_management.visual.sections.server.host')}
        placeholder="0.0.0.0"
        value={values.host}
        onChange={(e) => onChange({ host: e.target.value })}
        disabled={disabled}
        topExtra={topExtra}
      />
    </FieldAnchor>
  );
}

export function PortField({
  values,
  disabled,
  onChange,
  error,
  topExtra,
}: SharedFieldProps & { error?: string }) {
  const { t } = useTranslation();
  return (
    <FieldAnchor fieldId="port">
      <Input
        label={t('config_management.visual.sections.server.port')}
        type="number"
        placeholder="8317"
        value={values.port}
        onChange={(e) => onChange({ port: e.target.value })}
        disabled={disabled}
        error={error}
        topExtra={topExtra}
      />
    </FieldAnchor>
  );
}

export function ProxyUrlField({ values, disabled, onChange }: SharedFieldProps) {
  const { t } = useTranslation();
  // Long proxy URLs span both columns; sponsorship is optional.
  const sponsor = SPONSORS[0];
  return (
    <FieldAnchor fieldId="proxyUrl" wide>
      <Input
        label={t('config_management.visual.sections.network.proxy_url')}
        labelExtra={
          sponsor ? (
            <p className={fieldStyles.fieldSponsorHint}>
              {t('config_management.visual.sections.network.proxy_url_sponsor_hint')}{' '}
              <a
                className={fieldStyles.fieldSponsorLink}
                href={sponsor.url}
                target="_blank"
                rel="noopener noreferrer sponsored"
              >
                {sponsor.logo ? (
                  <img className={fieldStyles.fieldSponsorLogo} src={sponsor.logo} alt="" />
                ) : null}
                {sponsor.name}
              </a>
            </p>
          ) : undefined
        }
        placeholder="socks5://user:pass@127.0.0.1:1080/"
        value={values.proxyUrl}
        onChange={(e) => onChange({ proxyUrl: e.target.value })}
        disabled={disabled}
      />
    </FieldAnchor>
  );
}

/**
 * Align adjacent inputs with the optional sponsorship row above the proxy URL.
 */
export function SponsorHintSpacer() {
  if (SPONSORS.length === 0) return null;
  return (
    <p className={fieldStyles.fieldSponsorSpacer} aria-hidden="true">
      &nbsp;
    </p>
  );
}

export function ApiKeysField({ values, disabled, onChange }: SharedFieldProps) {
  const company = useConfigStore((state) => isCompanyMode(state.config));
  const apiBase = useAuthStore((state) => state.apiBase);
  return (
    <FieldAnchor fieldId="apiKeys" wide={company}>
      <FieldGroup>
        {company ? (
          <EmployeeKeysEditor key={apiBase} disabled={disabled} />
        ) : (
          <ApiKeysCardEditor
            value={values.apiKeysText}
            disabled={disabled}
            onChange={(apiKeysText) => onChange({ apiKeysText })}
          />
        )}
      </FieldGroup>
    </FieldAnchor>
  );
}

export function DebugToggle({ values, disabled, onChange }: SharedFieldProps) {
  const { t } = useTranslation();
  return (
    <FieldAnchor fieldId="debug">
      <ToggleRow
        title={t('config_management.visual.sections.system.debug')}
        description={t('config_management.visual.sections.system.debug_desc')}
        checked={values.debug}
        disabled={disabled}
        onChange={(debug) => onChange({ debug })}
      />
    </FieldAnchor>
  );
}

export function LoggingToFileToggle({ values, disabled, onChange }: SharedFieldProps) {
  const { t } = useTranslation();
  return (
    <FieldAnchor fieldId="loggingToFile">
      <ToggleRow
        title={t('config_management.visual.sections.system.logging_to_file')}
        description={t('config_management.visual.sections.system.logging_to_file_desc')}
        checked={values.loggingToFile}
        disabled={disabled}
        onChange={(loggingToFile) => onChange({ loggingToFile })}
      />
    </FieldAnchor>
  );
}

export function QuotaSwitchProjectToggle({ values, disabled, onChange }: SharedFieldProps) {
  const { t } = useTranslation();
  return (
    <FieldAnchor fieldId="quotaSwitchProject">
      <ToggleRow
        title={t('config_management.visual.sections.quota.switch_project')}
        description={t('config_management.visual.sections.quota.switch_project_desc')}
        checked={values.quotaSwitchProject}
        disabled={disabled}
        onChange={(quotaSwitchProject) => onChange({ quotaSwitchProject })}
      />
    </FieldAnchor>
  );
}

export function QuotaSwitchPreviewModelToggle({ values, disabled, onChange }: SharedFieldProps) {
  const { t } = useTranslation();
  return (
    <FieldAnchor fieldId="quotaSwitchPreviewModel">
      <ToggleRow
        title={t('config_management.visual.sections.quota.switch_preview_model')}
        description={t('config_management.visual.sections.quota.switch_preview_model_desc')}
        checked={values.quotaSwitchPreviewModel}
        disabled={disabled}
        onChange={(quotaSwitchPreviewModel) => onChange({ quotaSwitchPreviewModel })}
      />
    </FieldAnchor>
  );
}
