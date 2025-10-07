import { useEffect, useState } from "react";
import { apiClient } from "../App";

async function create(token, challenge) {}

const ChallengePanel = ({account}) => {
	const [challengeHash, setChallenge] = useState("");
	const [challenges, setChallenges] = useState([]);


	useEffect(() => {
		(async () => {
			const res = await apiClient.get('/challenges');
			const data = res.data;
			if (data.error) {
				console.error(data);
				return;
			}

			setChallenges(data.challenges);
		})();
	}, []);

	if (!account.name) {
		return <div className="challenge_panel unauthorized">
			<span>Please log in first</span>
		</div>;
	}

	const onChallengeChange = async (e) => {};

	const handleControlPanel = async (e) => {
		e.preventDefault();
	};

	return <div className="challenge_panel">
		<div>
			<select onChange={onChallengeChange}>
				{challenges.filter(x => x).map(x => <option key={x} value={x}>{x}</option>)}
			</select>
		</div>
		<div>
			<form className="control_panel" onSubmit={handleControlPanel}>
				<input type="submit" value="Start" />
				<input type="submit" value="Stop" />
				<input type="submit" value="Flag" />
			</form>
		</div>
	</div>;
};

export default ChallengePanel;