import { useState, type FormEvent } from 'react';

export function useForm<T>(onSubmit: (data: T) => Promise<void>) {
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  const handleSubmit = async (event: FormEvent<HTMLFormElement>, data: T) => {
    event.preventDefault();
    if (submitting) return;
    setError('');
    setSubmitting(true);
    try {
      await onSubmit(data);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '操作失败');
    } finally {
      setSubmitting(false);
    }
  };

  return { handleSubmit, submitting, error, setError };
}
