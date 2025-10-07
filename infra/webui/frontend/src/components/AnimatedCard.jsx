import {useEffect, useRef} from 'react';
import anime from 'animejs';

const AnimatedCard = ({children}) => {
	const cardRef = useRef(null);

	useEffect(() => {
		if (!cardRef.current) {
			return undefined;
		}

		const animation = anime({
			targets: cardRef.current,
			translateY: [
				24, 0
			],
			opacity: [
				0, 1
			],
			duration: 900,
			easing: 'spring(1, 80, 10, 0)'
		});

		return() => {
			animation.pause();
		};
	}, []);

	return (
		<section ref={cardRef} className="animated-card">
			{children}
		</section>
	);
};

export default AnimatedCard;
