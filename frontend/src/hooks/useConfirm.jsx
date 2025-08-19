import { useState } from 'react';
import { Modal } from '@/components/ui/Modal.jsx';
import { Button } from '@/components/ui/Button.jsx';

export function useConfirm() {
  const [state, setState] = useState({
    open: false,
    title: '',
    message: '',
    confirmText: 'Confirm',
    cancelText: 'Cancel',
    resolve: null,
  });

  function confirm(options) {
    return new Promise((resolve) => {
      setState({
        open: true,
        title: options?.title || '',
        message: options?.message || '',
        confirmText: options?.confirmText || 'Confirm',
        cancelText: options?.cancelText || 'Cancel',
        resolve,
      });
    });
  }

  function handleClose(result) {
    state.resolve?.(result);
    setState((s) => ({ ...s, open: false }));
  }

  const ConfirmModal = (
    <Modal open={state.open} onClose={() => handleClose(false)} aria-labelledby="confirm-title">
      <div className="space-y-md">
        {state.title && (
          <h2 id="confirm-title" className="text-lg font-medium">
            {state.title}
          </h2>
        )}
        {state.message && <p className="text-sm">{state.message}</p>}
        <div className="flex justify-end gap-sm pt-sm">
          <Button variant="secondary" onClick={() => handleClose(false)}>
            {state.cancelText}
          </Button>
          <Button onClick={() => handleClose(true)}>{state.confirmText}</Button>
        </div>
      </div>
    </Modal>
  );

  return { confirm, ConfirmModal };
}

export default useConfirm;
