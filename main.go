package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin" // Importamos Gin
	_ "github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

// --- ESTRUCTURAS DE DATOS (MODELOS) ---

// Empleado representa la estructura de datos que enviaremos como JSON
type Empleado struct {
	EmpNo     int    `json:"id"`
	FirstName string `json:"nombre"`
	LastName  string `json:"apellido"`
	Puesto    string `json:"puesto"`
	Salario   int    `json:"salario"`
}

// NuevoEmpleado sirve para recibir los datos desde el cliente
type NuevoEmpleado struct {
	FirstName string `json:"nombre"`
	LastName  string `json:"apellido"`
	Puesto    string `json:"puesto"`
	Salario   int    `json:"salario"`
}

// ActualizarEmpleado sirve para recibir los cambios de puesto o salario
type ActualizarEmpleado struct {
	Puesto  string `json:"puesto"`
	Salario int    `json:"salario"`
}

// LoginRequest sirve para recibir las credenciales universales de inicio de sesión
type LoginRequest struct {
	Correo     string `json:"correo"`
	Contrasena string `json:"contrasena"`
}

// RegistroUsuario sirve para recibir los datos de un nuevo acceso creado por el Admin
type RegistroUsuario struct {
	Correo     string `json:"correo"`
	Contrasena string `json:"contrasena"`
	Rol        string `json:"rol"`
	EmpleadoID *int   `json:"empleado_id"` // Usamos un puntero (*int) para que acepte nulos en la DB si es Admin
}

// --- MIDDLEWARE DE SEGURIDAD (INTERCEPTOR EN EL BACKEND) ---

func AuthMiddleware(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// El cliente (Android/Web) enviará el correo en la cabecera "Authorization"
		tokenHeader := c.GetHeader("Authorization")

		if tokenHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Acceso denegado. Token faltante."})
			c.Abort() // Detiene la petición inmediatamente
			return
		}

		// Validar si el usuario que intenta acceder realmente existe en la base de datos de Aiven
		var existe bool
		err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM usuarios WHERE correo = ?)", tokenHeader).Scan(&existe)
		if err != nil || !existe {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Token inválido o usuario no autorizado."})
			c.Abort()
			return
		}

		c.Next() // Permite continuar a la ruta si todo está bien
	}
}

func main() {
	// Cargar las variables del archivo .env (solo servirá en local, en Render se lo saltará pacíficamente)
	_ = godotenv.Load()

	// 1. Leer las credenciales
	usuario := os.Getenv("DB_USER")
	contrasena := os.Getenv("DB_PASSWORD")
	host := os.Getenv("DB_HOST")
	puerto := os.Getenv("DB_PORT")
	baseDatos := os.Getenv("DB_NAME")

	// 2. Armar la cadena de conexión
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?tls=skip-verify&parseTime=true",
		usuario, contrasena, host, puerto, baseDatos)

	// 3. Abrir la conexión
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Error al configurar la conexión: %v", err)
	}
	defer db.Close()

	// 4. Probar la conexión
	err = db.Ping()
	if err != nil {
		log.Fatalf("¡Error! No se pudo conectar a Aiven: %v", err)
	}

	fmt.Println("¡Conexión exitosa a la base de datos de Aiven! 🚀")

	// ==========================================
	// --- AQUÍ EMPIEZA API REST CON GIN ---
	// ==========================================

	// Crear el servidor de Gin por defecto
	r := gin.Default()

	// Ruta de prueba pública: http://localhost:8080/ping
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"mensaje": "¡La API está viva y funcionando! ",
		})
	})

	// --- ENDPOINT UNIVERSAL DE LOGIN (PÚBLICO) ---
	r.POST("/login", func(c *gin.Context) {
		var req LoginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Datos inválidos"})
			return
		}

		var dbContrasena string
		var dbRol string
		var dbEmpleadoID sql.NullInt32 // NullInt32 maneja correctamente el valor NULL del Administrador

		// Buscar las credenciales en la tabla que creamos juntos
		err := db.QueryRow("SELECT contrasena, rol, empleado_id FROM usuarios WHERE correo = ?", req.Correo).
			Scan(&dbContrasena, &dbRol, &dbEmpleadoID)

		if err == sql.ErrNoRows || dbContrasena != req.Contrasena {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Correo o contraseña incorrectos"})
			return
		} else if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error en el servidor: " + err.Error()})
			return
		}

		// Si el empleado_id es NULL, lo transformamos a 0 para el JSON
		var empID int = 0
		if dbEmpleadoID.Valid {
			empID = int(dbEmpleadoID.Int32)
		}

		// Devolvemos la respuesta con el rol y el id vinculado
		c.JSON(http.StatusOK, gin.H{
			"mensaje":     "¡Bienvenido!",
			"token":       req.Correo,
			"rol":         dbRol,
			"empleado_id": empID,
		})
	})

	// =========================================================================
	// --- RUTAS PROTEGIDAS POR EL MIDDLEWARE (Requieren cabecera Authorization) ---
	// =========================================================================
	api := r.Group("/")
	api.Use(AuthMiddleware(db))
	{
		// --- RUTA 1: CONSULTA DE PERSONAL (READ) ---
		api.GET("/empleados", func(c *gin.Context) {
			query := `
                SELECT e.emp_no, e.first_name, e.last_name, t.title, s.salary 
                FROM employees e
                INNER JOIN titles t ON e.emp_no = t.emp_no
                INNER JOIN salaries s ON e.emp_no = s.emp_no
                WHERE t.to_date = '9999-01-01' AND s.to_date = '9999-01-01'
                ORDER BY e.emp_no DESC
                LIMIT 20;
            `

			rows, err := db.Query(query)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al consultar la base de datos: " + err.Error()})
				return
			}
			defer rows.Close()

			var listaEmpleados []Empleado

			for rows.Next() {
				var emp Empleado
				err := rows.Scan(&emp.EmpNo, &emp.FirstName, &emp.LastName, &emp.Puesto, &emp.Salario)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al leer los datos: " + err.Error()})
					return
				}
				listaEmpleados = append(listaEmpleados, emp)
			}

			c.JSON(http.StatusOK, listaEmpleados)
		})

		// --- RUTA 2: REGISTRAR EMPLEADO (CREATE) ---
		api.POST("/empleados", func(c *gin.Context) {
			var datos NuevoEmpleado

			if err := c.ShouldBindJSON(&datos); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Datos inválidos: " + err.Error()})
				return
			}

			var ultimoID int
			err := db.QueryRow("SELECT MAX(emp_no) FROM employees").Scan(&ultimoID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al calcular el nuevo ID: " + err.Error()})
				return
			}
			nuevoID := ultimoID + 1

			_, err = db.Exec(`
                INSERT INTO employees (emp_no, birth_date, first_name, last_name, gender, hire_date) 
                VALUES (?, '1995-01-01', ?, ?, 'M', CURDATE())`,
				nuevoID, datos.FirstName, datos.LastName,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al crear empleado: " + err.Error()})
				return
			}

			_, err = db.Exec(`
                INSERT INTO titles (emp_no, title, from_date, to_date) 
                VALUES (?, ?, CURDATE(), '9999-01-01')`,
				nuevoID, datos.Puesto,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al asignarse puesto: " + err.Error()})
				return
			}

			_, err = db.Exec(`
                INSERT INTO salaries (emp_no, salary, from_date, to_date) 
                VALUES (?, ?, CURDATE(), '9999-01-01')`,
				nuevoID, datos.Salario,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al asignar salario: " + err.Error()})
				return
			}

			c.JSON(http.StatusCreated, gin.H{
				"mensaje":     "¡Empleado registrado con éxito en Coffeet! ☕",
				"id_generado": nuevoID,
			})
		})

		// --- RUTA 3: ACTUALIZAR PUESTO Y SALARIO (UPDATE) ---
		api.PUT("/empleados/:id", func(c *gin.Context) {
			idEmpleado := c.Param("id")
			var datos ActualizarEmpleado

			if err := c.ShouldBindJSON(&datos); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Datos inválidos: " + err.Error()})
				return
			}

			_, err := db.Exec(`
                UPDATE titles 
                SET title = ? 
                WHERE emp_no = ? AND to_date = '9999-01-01'`,
				datos.Puesto, idEmpleado,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al actualizar el puesto: " + err.Error()})
				return
			}

			_, err = db.Exec(`
                UPDATE salaries 
                SET salary = ? 
                WHERE emp_no = ? AND to_date = '9999-01-01'`,
				datos.Salario, idEmpleado,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al actualizar el salario: " + err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"mensaje": fmt.Sprintf("¡Datos del empleado %s actualizados con éxito! ☕", idEmpleado),
			})
		})

		// --- RUTA 4: BAJA DE PERSONAL (DELETE) ---
		api.DELETE("/empleados/:id", func(c *gin.Context) {
			idEmpleado := c.Param("id")

			_, err := db.Exec("DELETE FROM salaries WHERE emp_no = ?", idEmpleado)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al borrar salarios: " + err.Error()})
				return
			}

			_, err = db.Exec("DELETE FROM titles WHERE emp_no = ?", idEmpleado)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al borrar puestos: " + err.Error()})
				return
			}

			_, err = db.Exec("DELETE FROM employees WHERE emp_no = ?", idEmpleado)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al dar de baja al empleado: " + err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"mensaje": fmt.Sprintf("¡Empleado %s dado de baja correctamente de Coffeet! ☕", idEmpleado),
			})
		})

		// --- RUTA 5: REGISTRAR NUEVO ACCESO/USUARIO (CREATE - SOLO ADMIN) ---
		api.POST("/register", func(c *gin.Context) {
			// 1. Validar desde la base de datos que quien hace la petición sea un Administrador
			tokenHeader := c.GetHeader("Authorization") // Contiene el correo del administrador conectado
			var rolSolicitante string
			err := db.QueryRow("SELECT rol FROM usuarios WHERE correo = ?", tokenHeader).Scan(&rolSolicitante)
			if err != nil || rolSolicitante != "admin" {
				c.JSON(http.StatusForbidden, gin.H{"error": "Permisos insuficientes. Solo los administradores pueden registrar usuarios."})
				return
			}

			// 2. Mapear los datos JSON que vienen desde la app Android
			var req RegistroUsuario
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Datos inválidos: " + err.Error()})
				return
			}

			// 3. Validar si el correo ya existe para evitar duplicados
			var existe bool
			_ = db.QueryRow("SELECT EXISTS(SELECT 1 FROM usuarios WHERE correo = ?)", req.Correo).Scan(&existe)
			if existe {
				c.JSON(http.StatusConflict, gin.H{"error": "El correo ya se encuentra registrado."})
				return
			}

			// 4. Insertar el nuevo usuario en la tabla de usuarios
			query := "INSERT INTO usuarios (correo, contrasena, rol, empleado_id) VALUES (?, ?, ?, ?)"
			_, err = db.Exec(query, req.Correo, req.Contrasena, req.Rol, req.EmpleadoID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al guardar el usuario: " + err.Error()})
				return
			}

			c.JSON(http.StatusCreated, gin.H{
				"mensaje": "¡Usuario credencializado con éxito en Coffeet! ☕",
			})
		})
	}

	// --- CONFIGURACIÓN DINÁMICA DEL PUERTO PARA EL SERVIDOR (RENDER) ---
	puertoEnv := os.Getenv("PORT")
	if puertoEnv == "" {
		puertoEnv = "8080"
	}

	// Encender el servidor
	fmt.Printf("Servidor corriendo en el puerto %s \n", puertoEnv)
	err = r.Run(":" + puertoEnv)
	if err != nil {
		log.Fatalf("No se pudo encender el servidor: %v", err)
	}
}