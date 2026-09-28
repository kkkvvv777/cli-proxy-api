import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Select } from '@/components/ui/Select';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/Table';
import { IconDownload } from '@/components/ui/icons';
import { companyApi, type AuditEntry } from '@/services/api/company';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { getErrorMessage } from '@/utils/helpers';
import { useCompanySession } from './useCompanySession';
import styles from './Company.module.scss';

export function CompanyAuditPage() {
  const { t } = useTranslation();
  const current = useCompanySession();
  const [userId, setUserId] = useState('');
  const [limit, setLimit] = useState('100');
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [healthy, setHealthy] = useState<boolean | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const controller = useRef<AbortController | null>(null);

  const load = useCallback(
    async (employee = '', count = 100) => {
      controller.current?.abort();
      const active = new AbortController();
      controller.current = active;
      setLoading(true);
      setError('');
      try {
        const result = await companyApi.audit(employee, count, active.signal);
        if (!current() || active.signal.aborted) return;
        setEntries(result.entries);
        setHealthy(result.healthy);
      } catch (err) {
        if (current() && !active.signal.aborted)
          setError(getErrorMessage(err, t('company.load_failed')));
      } finally {
        if (current() && !active.signal.aborted) setLoading(false);
      }
    },
    [current, t]
  );
  useEffect(() => {
    void load();
    return () => controller.current?.abort();
  }, [load]);
  useHeaderRefresh(useCallback(() => load(userId, Number(limit)), [load, userId, limit]));

  const exportRecords = () => {
    const url = URL.createObjectURL(
      new Blob([JSON.stringify(entries, null, 2)], { type: 'application/json' })
    );
    const link = document.createElement('a');
    link.href = url;
    link.download = 'company-audit.json';
    link.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className={styles.page} data-testid="company-audit">
      <div className={styles.heading}>
        <h1>{t('company.audit')}</h1>
        {healthy !== null && (
          <span role="status" className={healthy ? styles.muted : styles.failed}>
            {t(healthy ? 'company.healthy' : 'company.unhealthy')}
          </span>
        )}
      </div>
      <form
        className={styles.toolbar}
        onSubmit={(event) => {
          event.preventDefault();
          void load(userId, Number(limit));
        }}
      >
        <Input
          label={t('company.id')}
          placeholder={t('company.all_employees')}
          value={userId}
          maxLength={64}
          onChange={(event) => setUserId(event.target.value)}
        />
        <label>
          {t('company.recent')}
          <Select
            ariaLabel={t('company.recent')}
            value={limit}
            onChange={setLimit}
            options={['100', '500', '1000'].map((value) => ({ value, label: value }))}
          />
        </label>
        <Button type="submit" loading={loading}>
          {t('company.search')}
        </Button>
        <Button
          type="button"
          variant="secondary"
          disabled={loading || Boolean(error) || !entries.length}
          onClick={exportRecords}
        >
          <IconDownload size={16} /> {t('company.export')}
        </Button>
      </form>
      {error && (
        <div className="error-box" role="alert">
          {error}
        </div>
      )}
      <Table className={styles.auditTable}>
        <TableHeader>
          <TableRow>
            {[
              'time',
              'employee',
              'kind',
              'models',
              'status',
              'tokens',
              'latency',
              'request_id',
            ].map((key) => (
              <TableHead key={key}>{t(`company.${key}`)}</TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {entries.map((entry, index) => (
            <TableRow key={`${entry.requestId}-${index}`}>
              <TableCell>{new Date(entry.timestamp).toLocaleString()}</TableCell>
              <TableCell>
                {entry.userName || entry.userId}
                <div className={styles.muted}>{entry.userId}</div>
              </TableCell>
              <TableCell>
                {t(`company.${entry.kind}`)}
                {entry.stream && <div className={styles.muted}>{t('company.stream')}</div>}
              </TableCell>
              <TableCell className={styles.model}>
                {entry.requestedModel}
                <div className={styles.muted}>
                  {[entry.model, entry.provider].filter(Boolean).join(' / ')}
                </div>
              </TableCell>
              <TableCell className={entry.failed ? styles.failed : styles.success}>
                {t(entry.failed ? 'company.failure' : 'company.success')} · {entry.statusCode}
              </TableCell>
              <TableCell>
                {entry.kind === 'attempt'
                  ? `${entry.inputTokens} / ${entry.outputTokens} / ${entry.totalTokens}`
                  : '—'}
              </TableCell>
              <TableCell>{entry.latencyMs} ms</TableCell>
              <TableCell className={styles.requestId}>{entry.requestId}</TableCell>
            </TableRow>
          ))}
          {!entries.length && (
            <TableRow>
              <TableCell colSpan={8} className={styles.empty}>
                {t(loading ? 'common.loading' : 'company.empty_audit')}
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  );
}
