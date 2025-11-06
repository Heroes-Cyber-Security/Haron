import { createPortal } from 'react-dom';
import '../styles/notification.scss';
import { useState } from 'react';

const Notification = ({ message, isFlag, onClose, onCopy }) => {
	if (!message) return null;
	const [isCopied, setCopied] = useState(false);

	const handleCopy = async () => {
		if (onCopy) {
			await onCopy();
			setCopied(true);
			setTimeout(() => setCopied(false), 2000);
		}
	};

	return createPortal(
		<>
			<div className="notification_overlay" onClick={onClose}></div>
			<div className="notification_modal">
				<div className="notification_text">
					<b>{isFlag ? "Flag Retrieved!" : "Notification"}</b>
					<p>{message}</p>
				</div>
				{isFlag && (
					<div className="notification_actions">
						<button className="copy_button" onClick={handleCopy}>
							{isCopied ? "Copied!" : "Copy Flag"}
						</button>
						<button className="close_button" onClick={onClose}>
							Close
						</button>
					</div>
				)}
				{!isFlag && (
					<button className="notification_close" onClick={onClose}>
						Close
					</button>
				)}
			</div>
		</>,
		document.body
	);
};

export default Notification;
