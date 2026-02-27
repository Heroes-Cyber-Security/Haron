import { useEffect, useState } from "react";
import { apiClient } from "../App";
import Markdown from "./Markdown";
import Notification from "./Notification";
import {copyToClipboard} from "../lib/clipboard";

const ChallengePanel = ({account, instance, setInstance, setFlag}) => {
	const [challengeHash, setChallenge] = useState("");
	const [readme, setReadme] = useState("");
	const [challenges, setChallenges] = useState([]);
	const [readmes, setReadmes] = useState([]);
	const [notification, setNotification] = useState({ message: "", isFlag: false });

	useEffect(() => {
		(async () => {
			const res = await apiClient.get('/challenges');
			const data = res.data;
			if (data.error) {
				console.error(data);
				return;
			}

			setChallenges(data.challenges);
			setReadmes(data.readmes);

			setChallenge(data.challenges[0]);
			setReadme(data.readmes[0]);
		})();
	}, []);

	if (!account.name) {
		return <div className="challenge_panel unauthorized">
			<span>Please log in first</span>
		</div>;
	}

	const onChallengeChange = async (e) => {
		setChallenge(e.target.value);

		for (let i = 0; i < challenges.length; i++) {
			if (challenges[i] == e.target.value) {
				setReadme(readmes[i]);
				break;
			}
		}
	};

	const handleStart = async (e) => {
		setInstance(x => ({
			starting: true
		}));
		const res = await apiClient.post('/create', undefined, {
			headers: {
				Token: account.accessToken,
				Challenge: challengeHash
			}
		});

		const data = res.data;
		setInstance(x => ({
			...x,
			id: data.id,
			setup_address: data.setup_address,
			player_private_key: `0x${data.player_private_key}`
		}));
	}

	const handleStop = async (e) => {
		if (!instance.id) return;
		setInstance(x => ({
			stopping: true
		}));

		const res = apiClient.post('/stop', undefined, {
			headers: {
				Token: account.accessToken
			}
		});
		setInstance(x => ({}));
	}

	const handleFlag = async (e) => {
		if (!instance.id) return;
		setInstance(x => ({
			...x,
			loading: true
		}));
		
		const res = await apiClient.get('/flag', {
			headers: {
				Token: account.accessToken
			}
		});
		const data = res.data;

		setInstance(x => ({
			...x,
			loading: false
		}));

		if (data.error) {
			setNotification({
				message: data.error,
				isFlag: false
			});
			return;
		}

		const flag = data.flag;
		setFlag(flag);

		setNotification({
			message: flag,
			isFlag: true
		});
	}

	const handleCopyFlag = async () => {
		await copyToClipboard(notification.message);
	}

	const handleCloseNotification = () => {
		setNotification({ message: "", isFlag: false });
	}

	return (
		<>
			<div className="challenge_panel">
				<div>
					<div>
						<select onChange={onChallengeChange} disabled={!!(instance.id)}>
							{challenges.filter(x => x).map(x => <option key={x} value={x}>{x}</option>)}
						</select>
					</div>
					{/* { !!(instance.id) ? <small>You already have a running instance</small> : null } */}
					<div className="challenge_readme">
						<Markdown content={readme} />
					</div>
				</div>
				<div>
					<form className="control_panel" onSubmit={e => e.preventDefault()}>
						<input
							type="submit"
							value={instance.starting ? "Starting..." : "Start"}
							disabled={!!instance.id || !!instance.starting}
							onClick={handleStart}
						/>
						<input
							type="submit"
							value={instance.stopping ? "Stopping..." : "Stop"}
							disabled={!instance.id || !!instance.stopping}
							onClick={handleStop}
						/>
						<input
							type="submit"
							value={instance.loading ? "Fetching..." : "Flag"}
							disabled={!instance.id || !!instance.loading}
							onClick={handleFlag}
						/>
					</form>
				</div>
			</div>
			<Notification
				message={notification.message}
				isFlag={notification.isFlag}
				onClose={handleCloseNotification}
				onCopy={handleCopyFlag}
			/>
		</>
	);
};

export default ChallengePanel;