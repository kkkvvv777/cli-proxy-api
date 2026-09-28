import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { ToggleSwitch } from '@/components/ui/ToggleSwitch';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/Table';
import { IconKey, IconPlus, IconRefreshCw, IconTrash2 } from '@/components/ui/icons';
import { companyApi, type Employee } from '@/services/api/company';
import { useNotificationStore } from '@/stores';
import { copyToClipboard } from '@/utils/clipboard';
import { getErrorMessage } from '@/utils/helpers';
import { useCompanySession } from './useCompanySession';
import styles from './Company.module.scss';

export function EmployeeKeysEditor({ disabled = false }: { disabled?: boolean }) {
  const { t } = useTranslation();
  const current = useCompanySession();
  const notify = useNotificationStore((state) => state.showNotification);
  const confirm = useNotificationStore((state) => state.showConfirmation);
  const [users, setUsers] = useState<Employee[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [edit, setEdit] = useState<Employee | 'new' | null>(null);
  const [id, setId] = useState('');
  const [name, setName] = useState('');
  const [newKey, setNewKey] = useState('');
  const [keyOwner, setKeyOwner] = useState('');
  const request = useRef(0);
  const mutation = useRef(false);

  const load = useCallback(
    async (signal?: AbortSignal) => {
      const sequence = ++request.current;
      setLoading(true);
      setError('');
      try {
        const data = await companyApi.list(signal);
        if (current() && !signal?.aborted && sequence === request.current) setUsers(data);
      } catch (err) {
        if (current() && !signal?.aborted && sequence === request.current)
          setError(getErrorMessage(err, t('company.load_failed')));
      } finally {
        if (current() && !signal?.aborted && sequence === request.current) setLoading(false);
      }
    },
    [current, t]
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => {
      controller.abort();
    };
  }, [load]);

  const run = async (action: () => Promise<void>) => {
    if (mutation.current || !current() || disabled) return;
    mutation.current = true;
    setBusy(true);
    setError('');
    try {
      await action();
      if (current()) await load();
    } catch (err) {
      if (current()) {
        const message = getErrorMessage(err, t('company.save_failed'));
        setError(message);
        notify(message, 'error');
      }
    } finally {
      mutation.current = false;
      if (current()) setBusy(false);
    }
  };

  const reveal = (result: Awaited<ReturnType<typeof companyApi.create>>) => {
    if (!current()) return;
    setKeyOwner(`${result.user.id} · ${result.user.name}`);
    setNewKey(result.apiKey);
  };
  const open = (user: Employee | 'new') => {
    setEdit(user);
    setId(user === 'new' ? '' : user.id);
    setName(user === 'new' ? '' : user.name);
  };
  const locked = disabled || busy || loading;
  const showAction = (user: Employee, kind: 'rotate' | 'delete') =>
    confirm({
      title: t(`company.${kind}`),
      message: t(`company.${kind}_confirm`, { name: user.name }),
      confirmText: t('common.confirm'),
      cancelText: t('common.cancel'),
      variant: 'danger',
      onConfirm: () =>
        run(async () => {
          if (kind === 'rotate') reveal(await companyApi.rotate(user.id));
          else await companyApi.remove(user.id);
        }),
    });

  return (
    <div className={styles.editor} data-testid="employee-keys">
      <div className={styles.heading}>
        <h3>{t('company.employees')}</h3>
        <div className={styles.actions}>
          <span className={styles.muted}>{t('company.count', { count: users.length })}</span>
          <Button
            type="button"
            variant="ghost"
            className={styles.iconButton}
            title={t('company.refresh')}
            aria-label={t('company.refresh')}
            disabled={locked}
            onClick={() => void load()}
          >
            <IconRefreshCw size={16} />
          </Button>
          <Button type="button" size="sm" disabled={locked} onClick={() => open('new')}>
            <IconPlus size={16} /> {t('company.create')}
          </Button>
        </div>
      </div>
      {error && (
        <div className="error-box" role="alert">
          {error}
        </div>
      )}
      <Table className={styles.employeeTable}>
        <TableHeader>
          <TableRow>
            {['id', 'name', 'prefix', 'enabled', 'actions'].map((key) => (
              <TableHead key={key}>{t(`company.${key}`)}</TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {users.map((user) => (
            <TableRow key={user.id}>
              <TableCell>{user.id}</TableCell>
              <TableCell>{user.name}</TableCell>
              <TableCell>
                <code>{user.keyPrefix}…</code>
              </TableCell>
              <TableCell>
                <ToggleSwitch
                  checked={user.enabled}
                  ariaLabel={`${t('company.enabled')} ${user.id}`}
                  disabled={locked}
                  onChange={(enabled) => void run(() => companyApi.update(user.id, { enabled }))}
                />
              </TableCell>
              <TableCell>
                <div className={styles.actions}>
                  <Button
                    type="button"
                    variant="secondary"
                    size="sm"
                    disabled={locked}
                    onClick={() => open(user)}
                  >
                    {t('company.rename')}
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    className={styles.iconButton}
                    disabled={locked}
                    title={t('company.rotate')}
                    aria-label={`${t('company.rotate')} ${user.id}`}
                    onClick={() => showAction(user, 'rotate')}
                  >
                    <IconKey size={16} />
                  </Button>
                  <Button
                    type="button"
                    variant="danger"
                    className={styles.iconButton}
                    disabled={locked}
                    title={t('company.delete')}
                    aria-label={`${t('company.delete')} ${user.id}`}
                    onClick={() => showAction(user, 'delete')}
                  >
                    <IconTrash2 size={16} />
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          ))}
          {!users.length && (
            <TableRow>
              <TableCell colSpan={5} className={styles.empty}>
                {t(loading ? 'common.loading' : 'company.empty_users')}
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
      <Modal
        open={edit !== null}
        onClose={() => setEdit(null)}
        closeDisabled={busy}
        title={t(edit === 'new' ? 'company.create' : 'company.rename')}
      >
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void run(async () => {
              if (edit === 'new') reveal(await companyApi.create(id.trim(), name.trim()));
              else if (edit) await companyApi.update(edit.id, { name: name.trim() });
              if (current()) setEdit(null);
            });
          }}
        >
          <Input
            label={t('company.id')}
            value={id}
            disabled={edit !== 'new' || busy}
            required
            maxLength={64}
            pattern="[a-zA-Z0-9][a-zA-Z0-9._\-]{0,63}"
            onChange={(event) => setId(event.target.value)}
          />
          <Input
            label={t('company.name')}
            value={name}
            required
            maxLength={66}
            disabled={busy}
            onChange={(event) => setName(event.target.value)}
          />
          <div className={styles.actions}>
            <Button type="button" variant="secondary" disabled={busy} onClick={() => setEdit(null)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" loading={busy}>
              {t('company.save')}
            </Button>
          </div>
        </form>
      </Modal>
      <Modal
        open={Boolean(newKey)}
        title={t('company.new_key')}
        onClose={() => setNewKey('')}
        footer={
          <>
            <Button
              type="button"
              variant="secondary"
              onClick={async () => {
                const copied = await copyToClipboard(newKey);
                notify(
                  t(copied ? 'notification.link_copied' : 'notification.copy_failed'),
                  copied ? 'success' : 'error'
                );
              }}
            >
              {t('common.copy')}
            </Button>
            <Button type="button" onClick={() => setNewKey('')}>
              {t('common.close')}
            </Button>
          </>
        }
      >
        <p>{keyOwner}</p>
        <Input label={t('company.new_key')} value={newKey} readOnly className={styles.secret} />
        <p>{t('company.secret_once')}</p>
      </Modal>
    </div>
  );
}
