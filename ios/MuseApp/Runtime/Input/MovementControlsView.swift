import UIKit

@MainActor
final class MovementControlsView: UIView {
    private(set) var currentInput = MovementInput.idle

    var scheme: MovementControlScheme = .gesture {
        didSet {
            guard scheme != oldValue else { return }
            applyScheme()
        }
    }

    private let joystickWell = UIView()
    private let joystickThumb = UIView()
    private var joystickOrigin: CGPoint?
    private var movePointer: UITouch?
    private var lookPointer: UITouch?
    private var lastLookLocation: CGPoint?
    private var pendingYawDelta: Float = 0

    private let assistiveStack = UIStackView()
    private var heldAssistiveInputs: [ObjectIdentifier: MovementInput] = [:]

    private var fadeOutTimer: Timer?
    private let joystickRadius: CGFloat = 60
    private let lookSensitivity: Float

    init(lookSensitivity: Float) {
        self.lookSensitivity = lookSensitivity
        super.init(frame: .zero)
        isMultipleTouchEnabled = true
        backgroundColor = .clear
        configureGestureControls()
        configureAssistiveControls()
        applyScheme()
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not supported")
    }

    func consumeInput() -> MovementInput {
        var input = currentInput
        input.yawDelta = pendingYawDelta
        pendingYawDelta = 0
        return input
    }

    // MARK: - Scheme

    private func applyScheme() {
        let isAssistive = scheme == .assistive
        assistiveStack.isHidden = !isAssistive
        joystickWell.isHidden = true
        resetAllInput()

        if isAssistive {
            fadeOutTimer?.invalidate()
            assistiveStack.alpha = 1
        }
    }

    private func resetAllInput() {
        currentInput = .idle
        pendingYawDelta = 0
        movePointer = nil
        lookPointer = nil
        lastLookLocation = nil
        joystickOrigin = nil
        heldAssistiveInputs.removeAll()
    }

    // MARK: - Gesture scheme

    private func configureGestureControls() {
        joystickWell.backgroundColor = UIColor.label.withAlphaComponent(0.12)
        joystickWell.layer.cornerRadius = joystickRadius
        joystickWell.isUserInteractionEnabled = false
        joystickWell.isHidden = true
        joystickWell.frame = CGRect(x: 0, y: 0, width: joystickRadius * 2, height: joystickRadius * 2)
        addSubview(joystickWell)

        joystickThumb.backgroundColor = UIColor.label.withAlphaComponent(0.45)
        joystickThumb.layer.cornerRadius = 26
        joystickThumb.frame = CGRect(x: 0, y: 0, width: 52, height: 52)
        joystickWell.addSubview(joystickThumb)
        joystickThumb.center = CGPoint(x: joystickRadius, y: joystickRadius)
    }

    override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent?) {
        super.touchesBegan(touches, with: event)
        guard scheme == .gesture else { return }

        for touch in touches {
            let location = touch.location(in: self)
            if location.x < bounds.midX, movePointer == nil {
                movePointer = touch
                joystickOrigin = location
                showJoystick(at: location)
            } else if lookPointer == nil {
                lookPointer = touch
                lastLookLocation = location
            }
        }
        showControls()
    }

    override func touchesMoved(_ touches: Set<UITouch>, with event: UIEvent?) {
        super.touchesMoved(touches, with: event)
        guard scheme == .gesture else { return }

        for touch in touches {
            let location = touch.location(in: self)
            if touch === movePointer, let origin = joystickOrigin {
                updateJoystick(origin: origin, location: location)
            } else if touch === lookPointer, let last = lastLookLocation {
                pendingYawDelta += Float(location.x - last.x) * lookSensitivity
                lastLookLocation = location
            }
        }
    }

    override func touchesEnded(_ touches: Set<UITouch>, with event: UIEvent?) {
        super.touchesEnded(touches, with: event)
        endTouches(touches)
    }

    override func touchesCancelled(_ touches: Set<UITouch>, with event: UIEvent?) {
        super.touchesCancelled(touches, with: event)
        endTouches(touches)
    }

    private func endTouches(_ touches: Set<UITouch>) {
        guard scheme == .gesture else { return }

        for touch in touches {
            if touch === movePointer {
                movePointer = nil
                joystickOrigin = nil
                currentInput.forward = 0
                currentInput.strafe = 0
                hideJoystick()
            } else if touch === lookPointer {
                lookPointer = nil
                lastLookLocation = nil
            }
        }
        scheduleFadeOutIfIdle()
    }

    private func showJoystick(at location: CGPoint) {
        joystickWell.isHidden = false
        joystickWell.center = location
        joystickThumb.center = CGPoint(x: joystickRadius, y: joystickRadius)
    }

    private func hideJoystick() {
        joystickWell.isHidden = true
    }

    private func updateJoystick(origin: CGPoint, location: CGPoint) {
        let dx = location.x - origin.x
        let dy = location.y - origin.y
        let distance = (dx * dx + dy * dy).squareRoot()
        let clamped = min(distance, joystickRadius)
        let angle = atan2(dy, dx)

        joystickThumb.center = CGPoint(
            x: joystickRadius + cos(angle) * clamped,
            y: joystickRadius + sin(angle) * clamped
        )

        currentInput.strafe = Float(cos(angle) * clamped / joystickRadius)
        currentInput.forward = Float(-sin(angle) * clamped / joystickRadius)
    }

    // MARK: - Fading

    private func showControls() {
        fadeOutTimer?.invalidate()
        UIView.animate(withDuration: 0.12) { self.joystickWell.alpha = 1 }
    }

    private func scheduleFadeOutIfIdle() {
        guard movePointer == nil, lookPointer == nil else { return }
        fadeOutTimer?.invalidate()
        fadeOutTimer = Timer.scheduledTimer(
            timeInterval: 1.2,
            target: self,
            selector: #selector(handleFadeOut),
            userInfo: nil,
            repeats: false
        )
    }

    @objc private func handleFadeOut() {
        UIView.animate(withDuration: 0.4) { self.joystickWell.alpha = 0 }
    }

    // MARK: - Assistive scheme

    private func configureAssistiveControls() {
        let turnLeft = makeAssistiveButton(
            symbol: "arrow.counterclockwise",
            label: "Turn left",
            input: MovementInput(yawDelta: -1)
        )
        let back = makeAssistiveButton(symbol: "chevron.down", label: "Move backward", input: MovementInput(forward: -1))
        let forward = makeAssistiveButton(symbol: "chevron.up", label: "Move forward", input: MovementInput(forward: 1))
        let turnRight = makeAssistiveButton(
            symbol: "arrow.clockwise",
            label: "Turn right",
            input: MovementInput(yawDelta: 1)
        )

        assistiveStack.axis = .horizontal
        assistiveStack.spacing = 12
        assistiveStack.distribution = .fillEqually
        assistiveStack.translatesAutoresizingMaskIntoConstraints = false
        for button in [turnLeft, back, forward, turnRight] {
            assistiveStack.addArrangedSubview(button)
        }
        addSubview(assistiveStack)

        NSLayoutConstraint.activate([
            assistiveStack.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 24),
            assistiveStack.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -24),
            assistiveStack.bottomAnchor.constraint(equalTo: safeAreaLayoutGuide.bottomAnchor, constant: -80),
            assistiveStack.heightAnchor.constraint(equalToConstant: 64)
        ])
    }

    private func makeAssistiveButton(symbol: String, label: String, input: MovementInput) -> UIButton {
        var configuration = UIButton.Configuration.filled()
        configuration.image = UIImage(systemName: symbol)
        configuration.baseBackgroundColor = .label
        configuration.baseForegroundColor = .systemBackground
        configuration.cornerStyle = .large

        let button = UIButton(configuration: configuration)
        button.accessibilityLabel = label
        button.addTarget(self, action: #selector(handleAssistiveTouchDown(_:)), for: [.touchDown, .touchDragEnter])
        button.addTarget(
            self,
            action: #selector(handleAssistiveTouchUp(_:)),
            for: [.touchUpInside, .touchUpOutside, .touchCancel, .touchDragExit]
        )
        assistiveInputs[ObjectIdentifier(button)] = input
        return button
    }

    private var assistiveInputs: [ObjectIdentifier: MovementInput] = [:]

    @objc private func handleAssistiveTouchDown(_ sender: UIButton) {
        guard let input = assistiveInputs[ObjectIdentifier(sender)] else { return }
        heldAssistiveInputs[ObjectIdentifier(sender)] = input
        recomputeAssistiveInput()
    }

    @objc private func handleAssistiveTouchUp(_ sender: UIButton) {
        heldAssistiveInputs.removeValue(forKey: ObjectIdentifier(sender))
        recomputeAssistiveInput()
    }

    private func recomputeAssistiveInput() {
        var combined = MovementInput.idle
        for input in heldAssistiveInputs.values {
            combined.forward += input.forward
            combined.strafe += input.strafe
            combined.yawDelta += input.yawDelta
        }
        currentInput.forward = combined.forward
        currentInput.strafe = combined.strafe
        assistiveTurnRate = combined.yawDelta
    }

    private(set) var assistiveTurnRate: Float = 0

    override func point(inside point: CGPoint, with event: UIEvent?) -> Bool {
        if scheme == .assistive {
            return assistiveStack.frame.contains(point)
        }
        return true
    }
}
