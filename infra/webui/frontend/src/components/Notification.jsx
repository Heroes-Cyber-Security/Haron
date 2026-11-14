import { useState } from 'react';
import { createPortal } from 'react-dom';

import '../styles/notification.scss';

const Notification = ({ message, isFlag, onClose, onCopy }) => {
  const [isCopied, setCopied] = useState(false);

  if (!message) return null;

  const handleCopy = async () => {
    if (onCopy) {
      await onCopy();
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return createPortal(
    <>
      <button
        type='button'
        className='notification_overlay'
        aria-label='Close'
        onClick={onClose}
      ></button>
      <div className='notification_modal'>
        <div className='notification_text'>
          <b>{isFlag ? 'Flag Retrieved!' : 'Notification'}</b>
          <p>{message}</p>
        </div>
        {isFlag && (
          <div className='notification_actions'>
            <button type='button' className='copy_button' onClick={handleCopy}>
              {isCopied ? 'Copied!' : 'Copy Flag'}
            </button>
            <button type='button' className='close_button' onClick={onClose}>
              Close
            </button>
          </div>
        )}
        {!isFlag && (
          <button type='button' className='notification_close' onClick={onClose}>
            Close
          </button>
        )}
      </div>
    </>,
    document.body,
  );
};

export default Notification;
