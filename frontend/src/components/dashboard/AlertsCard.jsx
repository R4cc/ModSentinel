import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { AlertTriangle } from 'lucide-react';
import { Button } from '@/components/ui/Button.jsx';
import { getToken } from '@/lib/api.ts';

export default function AlertsCard({ error, onRetry }) {
  const [tokenMissing, setTokenMissing] = useState(false);
  const [dismissed, setDismissed] = useState(() => {
    try {
      return JSON.parse(sessionStorage.getItem('alerts-dismissed')) || {};
    } catch {
      return {};
    }
  });

  useEffect(() => {
    function check() {
      getToken()
        .then((t) => setTokenMissing(!t))
        .catch(() => setTokenMissing(true));
    }
    check();
    window.addEventListener('token-change', check);
    return () => window.removeEventListener('token-change', check);
  }, []);

  function handleDismiss(key) {
    const next = { ...dismissed, [key]: true };
    setDismissed(next);
    sessionStorage.setItem('alerts-dismissed', JSON.stringify(next));
  }

  const alerts = [];
  if (tokenMissing && !dismissed.token) {
    alerts.push({
      key: 'token',
      message: 'Modrinth token required.',
      actions: (
        <Link to='/settings'>
          <Button variant='outline' className='h-8 px-sm'>Open Settings</Button>
        </Link>
      ),
    });
  }
  if (error && !dismissed.error) {
    const message = error === 'rate limited' ? 'Rate limit hit.' : 'Failed to load data.';
    alerts.push({
      key: 'error',
      message,
      actions: (
        <Button variant='outline' onClick={onRetry} className='h-8 px-sm'>
          Retry
        </Button>
      ),
    });
  }

  if (alerts.length === 0) {
    return <p className='text-muted-foreground'>No alerts.</p>;
  }

  return (
    <div className='flex flex-col gap-sm' role='status'>
      {alerts.map(({ key, message, actions }) => (
        <div
          key={key}
          className='flex items-start gap-sm rounded-md border border-amber-500 bg-amber-50 p-sm text-amber-900 dark:border-amber-400 dark:bg-amber-950 dark:text-amber-100'
        >
          <AlertTriangle className='mt-0.5 h-4 w-4 shrink-0' />
          <span className='flex-1'>{message}</span>
          <div className='flex gap-xs'>
            {actions}
            <Button variant='outline' onClick={() => handleDismiss(key)} className='h-8 px-sm'>
              Dismiss
            </Button>
          </div>
        </div>
      ))}
    </div>
  );
}
